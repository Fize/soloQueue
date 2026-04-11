/**
 * ============================================
 * Agent Machine (Agent 生命周期状态机)
 * ============================================
 *
 * 【职责】
 * - 管理 Agent 的完整生命周期
 * - 协调 LLM 调用和任务委托
 * - 维护消息历史
 * - 管理子 Actor
 *
 * 【状态图】
 *
 *                         ┌─────────────────────────────────────────┐
 *                         │                                          │
 *                         │  ┌──────┐                               │
 *                         │  │ idle │                               │
 *                         │  └──┬───┘                               │
 *                         │     │ task                              │
 *                         │     ▼                                   │
 *                         │ ┌────────────┐                          │
 *                         │ │ processing │                          │
 *                         │ └─────┬──────┘                          │
 *                         │       │                                 │
 *                         │   ┌───┴───┬───────────┐                 │
 *                         │   │       │           │                 │
 *                         │   ▼       ▼           ▼                 │
 *                         │  llm   delegate   respond               │
 *                         │   │       │           │                 │
 *                         │   ▼       ▼           │                 │
 *                         │ waitingLLM delegating ▼                 │
 *                         │   │          │  ┌────────────┐          │
 *                         │   │          │  │ responding │          │
 *                         │   │          │  └─────┬──────┘          │
 *                         │   │          │        │ done             │
 *                         │   ▼          │        │                  │
 *                         │ result ──────┴────────┘                  │
 *                         │    │                                   │
 *                         │    ▼                                   │
 *                         │ ┌────────────┐                          │
 *                         └►│ processing │                          │
 *                           └────────────┘                          │
 *                                                                  │
 *   error ──▶ idle (记录错误)                                      │
 *   stop ──▶ stopped                                               │
 *   reset ──▶ idle                                                 │
 *                                                                  │
 * 【核心决策逻辑】                                                   │
 *                                                                  │
 *   processing 状态分析消息内容:                                     │
 *   1. 需要 AI 生成? ──▶ llm 事件 ──▶ waitingLLM                   │
 *   2. 需要委托子任务? ──▶ delegate 事件 ──▶ delegating            │
 *   3. 直接响应? ──▶ respond 事件 ──▶ responding                  │
 *                                                                  │
 * ============================================
 */

import { setup, assign, fromPromise } from 'xstate';
import { createActor, type ActorRefFrom } from 'xstate';
import { Logger } from '../logger/index.js';
import { llmConfigService } from '../llm/index.js';
import { DEFAULT_AGENT_DEFAULTS } from '../llm/defaults.js';

// ============== Types ==============

export interface AgentContext {
  // 基础信息
  agentId: string;
  teamId: string;

  // 消息相关
  messages: ChatMessage[];
  currentTask: Task | null;
  pendingTasks: Task[];

  // 子 Actor
  children: Map<string, ChildInfo>;
  pendingChildResults: Map<string, ChildResult>;

  // LLM 相关
  currentLLMCall: LLMCall | null;
  llmHistory: LLMCall[];

  // 配置
  model: string;
  temperature: number;
  maxTokens: number;
  systemPrompt: string;

  // 状态
  errorCount: number;
  lastError: string | null;
}

export interface ChatMessage {
  id: string;
  role: 'user' | 'assistant' | 'system';
  content: string;
  timestamp: number;
}

export interface Task {
  id: string;
  type: 'chat' | 'delegate' | 'internal';
  content: string;
  from: string;
  parentTaskId?: string;
  createdAt: number;
}

export interface ChildInfo {
  actorRef: ActorRefFrom<any>;
  taskId: string;
  createdAt: number;
}

export interface ChildResult {
  taskId: string;
  content: string;
  error?: string;
}

export interface LLMCall {
  id: string;
  prompt: string;
  model: string;
  startTime: number;
  endTime?: number;
  status: 'pending' | 'streaming' | 'completed' | 'failed';
  content: string;
  tokens?: number;
  error?: string;
}

// ============== Events ==============

export type AgentEvent =
  // 外部事件
  | { type: 'task'; content: string; from: string; taskId: string }
  | { type: 'stop'; reason?: string }

  // LLM 事件
  | { type: 'llm.start'; prompt: string }
  | { type: 'llm.chunk'; content: string }
  | { type: 'llm.complete'; content: string }
  | { type: 'llm.error'; error: string }

  // 委托事件
  | { type: 'delegate.start'; taskId: string; instruction: string }
  | { type: 'delegate.result'; taskId: string; content: string }
  | { type: 'delegate.error'; taskId: string; error: string }

  // 响应事件
  | { type: 'respond'; content: string }
  | { type: 'respond.complete' }

  // 内部事件
  | { type: 'error'; error: string }
  | { type: 'retry' }
  | { type: 'reset' };

// ============== Constants ==============

const DEFAULT_MODEL = 'deepseek-chat';
const DEFAULT_TEMPERATURE = 0.7;
const DEFAULT_MAX_TOKENS = 2000;
const MAX_ERROR_COUNT = 3;

// ============== Logger ==============

// 使用 Logger.system() 创建日志实例
const logger = Logger.system({ enableConsole: true, enableFile: true, minLevel: 'debug' });

// ============== Guards ==============

const guards = {
  // 检查是否需要重试
  canRetry: ({ context }: { context: AgentContext }) =>
    context.errorCount < MAX_ERROR_COUNT,

  // 检查是否有子任务完成
  hasChildResults: ({ context }: { context: AgentContext }) =>
    context.children.size > 0,

  // 检查是否超过最大错误数
  tooManyErrors: ({ context }: { context: AgentContext }) =>
    context.errorCount >= MAX_ERROR_COUNT,
};

// ============== Actions ==============

const actions = {
  // 添加助手消息
  addAssistantMessage: assign({
    messages: ({ context, event }: { context: AgentContext; event: any }) => {
      if (event.content) {
        return [...context.messages, {
          id: crypto.randomUUID(),
          role: 'assistant' as const,
          content: event.content,
          timestamp: Date.now(),
        }];
      }
      return context.messages;
    },
  }),

  // 添加用户消息
  addUserMessage: assign({
    messages: ({ context, event }: { context: AgentContext; event: any }) => {
      if (event.content) {
        return [...context.messages, {
          id: crypto.randomUUID(),
          role: 'user' as const,
          content: event.content,
          timestamp: Date.now(),
        }];
      }
      return context.messages;
    },
  }),

  // 记录当前任务
  setCurrentTask: assign({
    currentTask: ({ event }: { event: any }) => ({
      id: event.taskId,
      type: 'chat' as const,
      content: event.content,
      from: event.from,
      createdAt: Date.now(),
    }),
  }),

  // 添加待处理任务
  addPendingTask: assign({
    pendingTasks: ({ context, event }: { context: AgentContext; event: any }) => [
      ...context.pendingTasks,
      {
        id: event.taskId,
        type: 'chat' as const,
        content: event.content,
        from: event.from,
        createdAt: Date.now(),
      },
    ],
  }),

  // 清除任务
  clearTask: assign({
    currentTask: () => null,
    pendingTasks: ({ context }: { context: AgentContext }) =>
      context.pendingTasks.slice(1), // 保留队列中的下一个任务
  }),

  // 记录错误
  recordError: assign({
    errorCount: ({ context }: { context: AgentContext }) => context.errorCount + 1,
    lastError: ({ event }: { event: any }) => event.error || 'Unknown error',
  }),

  // 清除错误
  clearError: assign({
    errorCount: () => 0,
    lastError: () => null,
  }),

  // 清除错误消息（保留计数）
  clearLastError: assign({
    lastError: () => null,
  }),

  // 记录 LLM 调用
  setLLMCall: assign({
    currentLLMCall: ({ context }: { context: AgentContext }) => ({
      id: crypto.randomUUID(),
      prompt: '',
      model: context.model,
      startTime: Date.now(),
      status: 'pending' as const,
      content: '',
    }),
  }),

  // 更新 LLM 调用状态
  updateLLMCall: assign({
    currentLLMCall: ({ context, event }: { context: AgentContext; event: any }) => {
      if (!context.currentLLMCall) return null;

      if (event.type === 'llm.chunk') {
        return {
          ...context.currentLLMCall,
          content: context.currentLLMCall.content + event.content,
          status: 'streaming' as const,
        };
      }

      if (event.type === 'llm.complete') {
        return {
          ...context.currentLLMCall,
          content: event.content,
          status: 'completed' as const,
          endTime: Date.now(),
        };
      }

      if (event.type === 'llm.error') {
        return {
          ...context.currentLLMCall,
          status: 'failed' as const,
          error: event.error,
          endTime: Date.now(),
        };
      }

      return context.currentLLMCall;
    },
  }),

  // 添加子 Actor
  addChild: assign({
    children: ({ context, event }: { context: AgentContext; event: any }) => {
      const newChildren = new Map(context.children);
      newChildren.set(event.taskId, {
        actorRef: event.actorRef,
        taskId: event.taskId,
        createdAt: Date.now(),
      });
      return newChildren;
    },
  }),

  // 移除子 Actor
  removeChild: assign({
    children: ({ context, event }: { context: AgentContext; event: any }) => {
      const newChildren = new Map(context.children);
      newChildren.delete(event.taskId);
      return newChildren;
    },
  }),

  // 记录子任务结果
  recordChildResult: assign({
    pendingChildResults: ({ context, event }: { context: AgentContext; event: any }) => {
      const newResults = new Map(context.pendingChildResults);
      newResults.set(event.taskId, {
        taskId: event.taskId,
        content: event.content || '',
        error: event.error,
      });
      return newResults;
    },
  }),

  // 合并子任务结果
  mergeChildResults: assign({
    messages: ({ context }: { context: AgentContext }) => {
      if (context.pendingChildResults.size === 0) return context.messages;

      const results = Array.from(context.pendingChildResults.values());
      const mergedContent = results
        .map(r => `[${r.taskId}]: ${r.content}`)
        .join('\n\n');

      return [...context.messages, {
        id: crypto.randomUUID(),
        role: 'assistant' as const,
        content: `[Child Tasks Results]\n${mergedContent}`,
        timestamp: Date.now(),
      }];
    },
  }),

  // 清除子任务结果
  clearChildResults: assign({
    pendingChildResults: () => new Map<string, ChildResult>(),
  }),

  // 日志
  logTask: ({ context }: { context: AgentContext }) => {
    logger.info('Task received', {
      agentId: context.agentId,
      taskId: context.currentTask?.id,
      contentLength: context.currentTask?.content.length || 0,
    });
  },

  logStateTransition: ({ context, event }: { context: AgentContext; event: any }) => {
    logger.debug('State transition', {
      agentId: context.agentId,
      eventType: event.type,
      errorCount: context.errorCount,
    });
  },

  logError: ({ context }: { context: AgentContext }) => {
    logger.error('Agent error', {
      agentId: context.agentId,
      error: context.lastError,
      errorCount: context.errorCount,
    });
  },

  logLLMStart: ({ context }: { context: AgentContext }) => {
    logger.info('LLM call started', {
      agentId: context.agentId,
      model: context.model,
      promptLength: context.currentLLMCall?.prompt.length || 0,
    });
  },

  logLLMComplete: ({ context }: { context: AgentContext }) => {
    logger.info('LLM call completed', {
      agentId: context.agentId,
      duration: context.currentLLMCall?.endTime && context.currentLLMCall?.startTime
        ? context.currentLLMCall.endTime - context.currentLLMCall.startTime
        : 0,
      responseLength: context.currentLLMCall?.content.length || 0,
    });
  },

  logDelegate: ({ context }: { context: AgentContext }) => {
    logger.info('Delegate started', {
      agentId: context.agentId,
      childCount: context.children.size,
    });
  },

  logResponse: ({ context }: { context: AgentContext }) => {
    logger.info('Response sent', {
      agentId: context.agentId,
      messageCount: context.messages.length,
    });
  },

  logChildResult: ({ context, event }: { context: AgentContext; event: any }) => {
    logger.info('Child task result received', {
      agentId: context.agentId,
      taskId: event.taskId,
      hasError: !!event.error,
      contentLength: event.content?.length || 0,
    });
  },

  logAgentStart: ({ context }: { context: AgentContext }) => {
    logger.info('Agent started', {
      agentId: context.agentId,
      teamId: context.teamId,
      model: context.model,
      initialMessages: context.messages.length,
    });
  },
};

// ============== Actors ==============

const actors = {
  /**
   * 调用 LLM
   */
  callLLM: fromPromise(async ({ input }: { input: { prompt: string; context: AgentContext } }) => {
    const { prompt, context } = input;

    logger.info({
      category: 'actor',
      message: 'Calling LLM',
      actorId: context.agentId,
      context: {
        agentId: context.agentId,
        model: context.model,
        messageCount: context.messages.length,
      },
    });

    // 构建消息历史
    const historyMessages = context.messages.map(m => ({
      role: m.role,
      content: m.content,
    }));

    const response = await fetch('/api/llm/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        model: context.model,
        messages: [
          { role: 'system', content: context.systemPrompt },
          ...historyMessages,
          { role: 'user', content: prompt },
        ],
        temperature: context.temperature,
        max_tokens: context.maxTokens,
        stream: false,
      }),
    });

    if (!response.ok) {
      const errorData = await response.json().catch(() => ({}));
      throw new Error(errorData.message || `HTTP ${response.status}`);
    }

    const data = await response.json();

    return {
      content: data.content || data.choices?.[0]?.message?.content || '',
      usage: data.usage,
    };
  }),

  /**
   * 委托任务给子 Actor
   */
  delegateTask: fromPromise(async ({ input }: { input: { taskId: string; instruction: string; parentAgentId: string } }) => {
    // TODO: 实现任务委托逻辑
    // 1. 创建子 Agent (或使用现有的 Worker Agent)
    // 2. 发送任务
    // 3. 返回子任务 ID

    logger.info({
      category: 'actor',
      message: 'Delegating task',
      actorId: input.parentAgentId,
      context: {
        parentAgentId: input.parentAgentId,
        taskId: input.taskId,
        instructionLength: input.instruction.length,
      },
    });

    // 模拟委托
    return {
      taskId: input.taskId,
      status: 'delegated',
      childAgentId: `child-${input.taskId}`,
    };
  }),
};

// ============== Machine ==============

export const agentMachine = setup({
  types: {
    context: {} as AgentContext,
    events: {} as AgentEvent,
  },
  actions,
  guards,
  actors,
}).createMachine({
  id: 'agent',
  initial: 'idle',
  createdAt: new Date().toISOString(),

  context: ({ input }) => ({
    agentId: input?.agentId || '',
    teamId: input?.teamId || '',
    messages: input?.initialMessages || [],
    currentTask: null,
    pendingTasks: [],
    children: new Map(),
    pendingChildResults: new Map(),
    currentLLMCall: null,
    llmHistory: [],
    model: input?.model || DEFAULT_MODEL,
    temperature: input?.temperature ?? DEFAULT_TEMPERATURE,
    maxTokens: input?.maxTokens || DEFAULT_MAX_TOKENS,
    systemPrompt: input?.systemPrompt || '',
    errorCount: 0,
    lastError: null,
  }),

  states: {
    // ========== idle ==========
    idle: {
      entry: 'logAgentStart',
      on: {
        task: {
          target: 'processing',
          actions: ['addUserMessage', 'setCurrentTask', 'clearLastError', 'logTask'],
        },
      },
    },

    // ========== processing ==========
    processing: {
      entry: 'logStateTransition',
      on: {
        llm: {
          target: 'waitingLLM',
          actions: 'setLLMCall',
        },
        delegate: {
          target: 'delegating',
        },
        respond: {
          target: 'responding',
          actions: 'addAssistantMessage',
        },
        error: {
          target: 'idle',
          actions: ['recordError', 'logError'],
        },
      },
    },

    // ========== waitingLLM ==========
    waitingLLM: {
      entry: 'logLLMStart',
      invoke: {
        src: 'callLLM',
        input: ({ context }) => ({
          prompt: context.currentTask?.content || '',
          context,
        }),
        onDone: {
          target: 'processing',
          actions: ['addAssistantMessage', 'updateLLMCall', 'logLLMComplete'],
        },
        onError: {
          target: 'idle',
          actions: ['recordError', 'updateLLMCall', 'logError'],
        },
      },
      on: {
        'llm.chunk': {
          actions: 'updateLLMCall',
        },
        'llm.error': {
          target: 'idle',
          actions: ['recordError', 'updateLLMCall', 'logError'],
        },
      },
    },

    // ========== delegating ==========
    delegating: {
      entry: 'logDelegate',
      invoke: {
        src: 'delegateTask',
        input: ({ context, event }) => ({
          taskId: event.taskId,
          instruction: event.instruction,
          parentAgentId: context.agentId,
        }),
        onDone: {
          target: 'waitingChild',
          actions: 'addChild',
        },
        onError: {
          target: 'idle',
          actions: ['recordError', 'logError'],
        },
      },
    },

    // ========== waitingChild ==========
    waitingChild: {
      on: {
        'delegate.result': {
          target: 'processing',
          actions: ['recordChildResult', 'removeChild'],
        },
        'delegate.error': {
          target: 'idle',
          actions: ['recordChildResult', 'removeChild', 'recordError', 'logError'],
        },
      },
    },

    // ========== responding ==========
    responding: {
      entry: 'logResponse',
      on: {
        'respond.complete': {
          target: 'idle',
          actions: 'clearTask',
        },
      },
    },

    // ========== stopped ==========
    stopped: {
      type: 'final',
    },
  },
});

// ============== Factory ==============

export interface CreateAgentOptions {
  agentId: string;
  teamId: string;
  model?: string;
  temperature?: number;
  maxTokens?: number;
  systemPrompt?: string;
  initialMessages?: ChatMessage[];
}

/**
 * 创建 Agent Actor
 */
export function createAgentActor(options: CreateAgentOptions) {
  // 从配置服务获取默认值
  const agentDefaults = llmConfigService.getAgentDefaults?.() || DEFAULT_AGENT_DEFAULTS;
  const defaultChatConfig = agentDefaults.roleDefaults?.chat || { modelId: 'deepseek-chat', temperature: 0.7, maxTokens: 4096 };

  const {
    agentId,
    teamId,
    model = defaultChatConfig.modelId,
    temperature = defaultChatConfig.temperature,
    maxTokens = defaultChatConfig.maxTokens,
    systemPrompt = '',
    initialMessages = [],
  } = options;

  logger.info({
    category: 'actor',
    message: 'Creating agent actor',
    actorId: agentId,
    context: { agentId, teamId, model },
  });

  return createActor(agentMachine, {
    input: {
      agentId,
      teamId,
      model,
      temperature,
      maxTokens,
      systemPrompt,
      messages: initialMessages,
    },
  });
}

// ============== Exports ==============

export {
  DEFAULT_MODEL,
  DEFAULT_TEMPERATURE,
  DEFAULT_MAX_TOKENS,
  MAX_ERROR_COUNT,
};
