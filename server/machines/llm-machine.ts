/**
 * ============================================
 * LLM Machine (LLM 调用状态机)
 * ============================================
 *
 * 【职责】
 * - 管理 LLM 调用的完整生命周期
 * - 支持流式响应和普通响应
 * - 提供重试机制和超时控制
 * - 记录调用元数据 (耗时、token 数量)
 *
 * 【状态图】
 *
 *   idle ──call──▶ loading ──▶ success
 *                    │          ▲
 *                    │error     │
 *                    ▼          │
 *                  failed ──retry──┘
 *                    │
 *                    └──reset──▶ idle
 *
 * 【超时配置】
 *
 *   默认超时: 5 分钟 (300000ms)
 *   可配置项: timeout, maxRetries
 *
 * ============================================
 */

import { setup, assign, fromPromise } from 'xstate';
import { createActor } from 'xstate';
import { Logger } from '../logger/index.js';

// ============== Types ==============

export interface LLMContext {
  // 调用信息
  callId: string;
  model: string;
  messages: Array<{ role: string; content: string }>;
  temperature: number;
  maxTokens: number;

  // 响应相关
  fullResponse: string;
  currentChunk: string;

  // 元数据
  startTime: number | null;
  endTime: number | null;
  tokensUsed: number | null;
  duration: number | null;

  // 配置
  timeout: number;           // 超时时间 (ms)，默认 5 分钟
  maxRetries: number;         // 最大重试次数
  retryCount: number;        // 当前重试次数

  // 错误
  error: string | null;
}

export interface LLMInput {
  model: string;
  messages: Array<{ role: string; content: string }>;
  temperature: number;
  maxTokens: number;
  timeout?: number;           // 默认 300000 (5 分钟)
  signal?: AbortSignal;
}

export type LLMEvent =
  | { type: 'call'; input: LLMInput }
  | { type: 'chunk'; content: string }
  | { type: 'complete' }
  | { type: 'error'; message: string }
  | { type: 'retry' }
  | { type: 'reset' }
  | { type: 'cancel' };

// ============== Constants ==============

const DEFAULT_TIMEOUT = 5 * 60 * 1000;      // 5 分钟
const DEFAULT_MAX_RETRIES = 3;
const DEFAULT_MODEL = 'deepseek-chat';
const DEFAULT_TEMPERATURE = 0.7;
const DEFAULT_MAX_TOKENS = 2000;

// ============== Logger ==============

const logger = Logger.contextualize('[LLM Machine]');

// ============== Actors ==============

const actors = {
  /**
   * LLM 调用 (非流式)
   * 使用 AbortController 实现超时控制
   */
  llmCall: fromPromise(async ({ input }: { input: LLMInput }): Promise<{ content: string; usage?: { tokens: number } }> => {
    const { model, messages, temperature, maxTokens, timeout, signal } = input;

    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), timeout || DEFAULT_TIMEOUT);

    try {
      const response = await fetch('/api/llm/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          model,
          messages,
          temperature,
          max_tokens: maxTokens,
        }),
        signal: signal || controller.signal,
      });

      clearTimeout(timeoutId);

      if (!response.ok) {
        const errorData = await response.json().catch(() => ({}));
        throw new Error(errorData.message || `HTTP ${response.status}: ${response.statusText}`);
      }

      const data = await response.json();

      logger.info('LLM call completed', {
        callId: input.model,
        tokens: data.usage?.total_tokens,
      });

      return {
        content: data.content || data.choices?.[0]?.message?.content || '',
        usage: data.usage,
      };
    } catch (error: any) {
      clearTimeout(timeoutId);

      if (error.name === 'AbortError') {
        throw new Error(`LLM call timeout after ${timeout || DEFAULT_TIMEOUT}ms`);
      }

      logger.error('LLM call failed', { error: error.message });
      throw error;
    }
  }),
};

// ============== Guards ==============

const guards = {
  canRetry: ({ context }: { context: LLMContext }) => context.retryCount < context.maxRetries,
  hasError: ({ context }: { context: LLMContext }) => context.error !== null,
};

// ============== Actions ==============

const actions = {
  // 初始化调用
  initCall: assign(({ event }: { event: any }) => ({
    callId: crypto.randomUUID(),
    fullResponse: '',
    currentChunk: '',
    startTime: Date.now(),
    endTime: null,
    tokensUsed: null,
    duration: null,
    error: null,
    // 从输入中获取配置
    model: event.input?.model || DEFAULT_MODEL,
    messages: event.input?.messages || [],
    temperature: event.input?.temperature || DEFAULT_TEMPERATURE,
    maxTokens: event.input?.maxTokens || DEFAULT_MAX_TOKENS,
    timeout: event.input?.timeout || DEFAULT_TIMEOUT,
    maxRetries: DEFAULT_MAX_RETRIES,
    retryCount: 0,
  })),

  // 追加响应片段
  appendChunk: assign({
    fullResponse: ({ context, event }: { context: LLMContext; event: any }) =>
      context.fullResponse + event.content,
    currentChunk: ({ event }: { event: any }) => event.content,
  }),

  // 调用完成
  complete: assign(({ context, event }: { context: LLMContext; event: any }) => ({
    fullResponse: event.output?.content || context.fullResponse,
    tokensUsed: event.output?.usage?.tokens || null,
    currentChunk: '',
    endTime: Date.now(),
    duration: context.startTime ? Date.now() - context.startTime : null,
  })),

  // 记录错误
  setError: assign(({ context, event }: { context: LLMContext; event: any }) => ({
    error: event.message || context.error || 'Unknown error',
    endTime: Date.now(),
    duration: context.startTime ? Date.now() - context.startTime : null,
  })),

  // 重试计数
  incrementRetry: assign(({ context }: { context: LLMContext }) => ({
    retryCount: context.retryCount + 1,
    error: null,
    endTime: null,
    duration: null,
  })),

  // 重置
  reset: assign(() => ({
    callId: '',
    fullResponse: '',
    currentChunk: '',
    startTime: null,
    endTime: null,
    tokensUsed: null,
    duration: null,
    error: null,
    retryCount: 0,
  })),

  // 记录日志
  logStart: ({ context }: { context: LLMContext }) => {
    logger.info('LLM call started', {
      model: context.model,
      timeout: context.timeout,
      messageCount: context.messages.length,
    });
  },

  logRetry: ({ context }: { context: LLMContext }) => {
    logger.warn('LLM call retry', {
      retryCount: context.retryCount,
      maxRetries: context.maxRetries,
      error: context.error,
    });
  },

  logComplete: ({ context }: { context: LLMContext }) => {
    logger.info('LLM call completed', {
      callId: context.callId,
      duration: context.duration,
      tokensUsed: context.tokensUsed,
      responseLength: context.fullResponse.length,
    });
  },

  logError: ({ context }: { context: LLMContext }) => {
    logger.error('LLM call failed', {
      callId: context.callId,
      error: context.error,
      retryCount: context.retryCount,
    });
  },
};

// ============== Machine ==============

export const llmMachine = setup({
  types: {
    context: {} as LLMContext,
    events: {} as LLMEvent,
  },
  actions,
  guards,
  actors,
}).createMachine({
  id: 'llm',
  initial: 'idle',
  createdAt: new Date().toISOString(),

  context: {
    callId: '',
    model: DEFAULT_MODEL,
    messages: [],
    temperature: DEFAULT_TEMPERATURE,
    maxTokens: DEFAULT_MAX_TOKENS,
    fullResponse: '',
    currentChunk: '',
    startTime: null,
    endTime: null,
    tokensUsed: null,
    duration: null,
    timeout: DEFAULT_TIMEOUT,           // 5 分钟
    maxRetries: DEFAULT_MAX_RETRIES,
    retryCount: 0,
    error: null,
  },

  states: {
    // ========== idle ==========
    idle: {
      on: {
        call: {
          target: 'loading',
          actions: ['initCall', 'logStart'],
        },
      },
    },

    // ========== loading ==========
    loading: {
      invoke: {
        src: 'llmCall',
        input: ({ context }) => ({
          model: context.model,
          messages: context.messages,
          temperature: context.temperature,
          maxTokens: context.maxTokens,
          timeout: context.timeout,
        }),
        onDone: {
          target: 'success',
          actions: 'complete',
        },
        onError: {
          target: 'failed',
          actions: 'setError',
        },
      },
    },

    // ========== streaming ==========
    // 注: 流式响应通过外部 WebSocket 处理，本状态用于未来扩展
    streaming: {
      on: {
        chunk: {
          actions: 'appendChunk',
        },
        complete: {
          target: 'success',
          actions: assign({
            currentChunk: () => '',
            endTime: () => Date.now(),
          }),
        },
        error: {
          target: 'failed',
          actions: 'setError',
        },
      },
    },

    // ========== success ==========
    success: {
      entry: 'logComplete',
      on: {
        call: {
          target: 'loading',
          actions: ['reset', 'initCall', 'logStart'],
        },
        reset: {
          target: 'idle',
          actions: 'reset',
        },
      },
    },

    // ========== failed ==========
    failed: {
      entry: 'logError',
      on: {
        retry: {
          target: 'loading',
          actions: ['incrementRetry', 'logRetry'],
          guard: 'canRetry',
        },
        reset: {
          target: 'idle',
          actions: 'reset',
        },
      },
    },
  },
});

// ============== Factory ==============

export interface CreateLLMOptions {
  model?: string;
  temperature?: number;
  maxTokens?: number;
  timeout?: number;      // 默认 5 分钟
  maxRetries?: number;
}

/**
 * 创建 LLM Machine Actor
 */
export function createLLMActor(options: CreateLLMOptions = {}) {
  return createActor(llmMachine, {
    input: {
      model: options.model || DEFAULT_MODEL,
      temperature: options.temperature || DEFAULT_TEMPERATURE,
      maxTokens: options.maxTokens || DEFAULT_MAX_TOKENS,
      timeout: options.timeout || DEFAULT_TIMEOUT,
      maxRetries: options.maxRetries || DEFAULT_MAX_RETRIES,
    },
  });
}

// ============== Exports ==============

export { DEFAULT_TIMEOUT, DEFAULT_MAX_RETRIES };
