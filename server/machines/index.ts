/**
 * ============================================
 * 状态机模块 - 统一导出
 * ============================================
 *
 * 【模块结构】
 *
 *   server/machines/
 *   ├── index.ts           # 本文件 - 统一导出
 *   ├── agent-machine.ts   # Agent 生命周期状态机
 *   ├── llm-machine.ts     # LLM 调用状态机
 *   └── task-machine.ts    # 任务队列状态机
 *
 * 【设计原则】
 *
 *   1. Agent Machine 是主状态机，协调 LLM 和 Task
 *   2. LLM Machine 专门管理 AI 调用
 *   3. Task Machine 管理任务队列
 *   4. 三者通过事件相互通信
 *
 * 【配置默认值】
 *
 *   LLM:
 *   - timeout: 5 分钟 (300000ms)
 *   - maxRetries: 3
 *
 *   Task:
 *   - maxQueueSize: 100
 *   - maxRunning: 10
 *   - timeout: 30 秒
 *   - maxRetries: 3
 *   - priority: 5 (1-10)
 *
 *   Agent:
 *   - model: deepseek-chat
 *   - temperature: 0.7
 *   - maxTokens: 2000
 *   - maxErrors: 3
 *
 * 【使用示例】
 *
 *   import {
 *     createAgentActor,
 *     createLLMActor,
 *     createTaskActor,
 *     createTaskQueueActor,
 *   } from './machines';
 *
 *   // 创建 Agent
 *   const agent = createAgentActor({
 *     agentId: 'agent-001',
 *     teamId: 'team-001',
 *     model: 'deepseek-chat',
 *   });
 *   agent.start();
 *
 *   // 发送任务
 *   agent.send({
 *     type: 'task',
 *     content: '帮我写一个函数',
 *     from: 'user',
 *     taskId: 'task-001',
 *   });
 *
 * ============================================
 */

// Agent Machine
export {
  agentMachine,
  createAgentActor,
  DEFAULT_MODEL,
  DEFAULT_TEMPERATURE,
  DEFAULT_MAX_TOKENS,
  MAX_ERROR_COUNT,
} from './agent-machine.js';

export type {
  AgentContext,
  AgentEvent,
  ChatMessage,
  Task,
  ChildInfo,
  ChildResult,
  LLMCall,
  CreateAgentOptions,
} from './agent-machine.js';

// LLM Machine
export {
  llmMachine,
  createLLMActor,
  DEFAULT_TIMEOUT as LLM_TIMEOUT,
  DEFAULT_MAX_RETRIES as LLM_MAX_RETRIES,
} from './llm-machine.js';

export type {
  LLMContext,
  LLMInput,
  LLMEvent,
  CreateLLMOptions,
} from './llm-machine.js';

// Task Machine
export {
  taskMachine,
  taskQueueMachine,
  createTaskActor,
  createTaskQueueActor,
  DEFAULT_TIMEOUT as TASK_TIMEOUT,
  DEFAULT_MAX_RETRIES as TASK_MAX_RETRIES,
  DEFAULT_PRIORITY,
} from './task-machine.js';

export type {
  TaskType,
  TaskContext,
  TaskInput,
  TaskEvent,
  TaskQueueContext,
  TaskQueueEvent,
  CreateTaskOptions,
} from './task-machine.js';
