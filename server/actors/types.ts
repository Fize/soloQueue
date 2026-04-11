/**
 * ============================================
 * Actor 系统核心 - 类型定义
 * ============================================
 *
 * 【设计原则】
 * 1. 复用优于重写 - 直接使用已有的工厂函数和服务
 * 2. 扩展点设计 - 通过工厂模式支持新 Agent 类型
 * 3. 系统内置 Agent - 自动创建，不可删除
 *
 */

import type { ActorRefFrom } from 'xstate';
import type { agentMachine } from '../machines/agent-machine.js';
import type { llmMachine } from '../machines/llm-machine.js';
import type { taskMachine } from '../machines/task-machine.js';

// ============== Agent 角色 ==============

/**
 * Agent 角色 - 区分系统内置和用户创建
 */
export type AgentRole = 
  | 'system'     // 系统内置 Agent，不能删除
  | 'user';      // 用户创建的 Agent

/**
 * Agent 类型 - 定义 Agent 的行为能力
 */
export type AgentKind = 
  | 'chat'       // 通用对话
  | 'tool'       // 工具执行
  | 'code'       // 代码生成
  | 'planner'    // 任务规划
  | 'evaluator'  // 结果评估
  | 'system'     // 系统基础设施 (router/logger/persister)
  | 'custom';    // 自定义

// ============== 消息系统 ==============

/**
 * Actor 消息 - 所有 Actor 间消息的类型
 */
export type ActorMessage =
  | TaskMessage
  | DelegateMessage
  | ResultMessage
  | ErrorMessage
  | SystemMessage
  | SupervisionMessage;

/**
 * 任务消息 - 用户发起的任务请求
 */
export interface TaskMessage {
  type: 'task';
  taskId: string;
  content: string;
  from: string;
  replyTo?: string;
  priority?: number;
  metadata?: Record<string, unknown>;
}

/**
 * 委托消息 - Agent 间任务委托
 */
export interface DelegateMessage {
  type: 'delegate';
  taskId: string;
  instruction: string;
  to: string;
  from: string;
  metadata?: Record<string, unknown>;
}

/**
 * 结果消息 - 任务执行结果
 */
export interface ResultMessage {
  type: 'result';
  taskId: string;
  content: string;
  from: string;
  to: string;
  metadata?: Record<string, unknown>;
}

/**
 * 错误消息 - 错误通知
 */
export interface ErrorMessage {
  type: 'error';
  taskId?: string;
  error: string;
  from: string;
  to: string;
  recoverable: boolean;
}

/**
 * 系统消息 - 系统控制
 */
export interface SystemMessage {
  type: 'system';
  action: 'ping' | 'pong' | 'status' | 'shutdown' | 'restart';
  target?: string;
  payload?: Record<string, unknown>;
}

/**
 * 监督消息 - Actor 系统内部使用
 */
export interface SupervisionMessage {
  type: 'supervision';
  action: 'started' | 'stopped' | 'failure' | 'restart' | 'escalate';
  childId?: string;
  parentId?: string;
  reason?: string;
}

// ============== 监督配置 ==============

/**
 * 监督策略
 */
export type SupervisionStrategy = 
  | 'one_for_one'    // 一个失败，只重启该 Actor
  | 'one_for_all'    // 一个失败，重启所有子 Actor
  | 'all_for_one'    // 一个失败，全部重启
  | 'stop';          // 不重启，停止子树

/**
 * 监督配置
 */
export interface SupervisionConfig {
  strategy: SupervisionStrategy;
  maxRetries: number;           // 最大重启次数
  retryInterval: number;        // 重试间隔 (ms)
  exponentialBackoff: boolean;  // 指数退避
  maxBackoff: number;          // 最大退避时间 (ms)
}

// ============== Agent 定义 ==============

/**
 * Agent 定义 - 存储在数据库中的配置
 */
export interface AgentDefinition {
  id: string;
  name: string;
  teamId: string;
  
  // 角色和类型
  role: AgentRole;
  kind: AgentKind;
  
  // 模型配置
  modelId: string;        // 模型 ID，如 'deepseek-chat'
  providerId: string;    // 提供商 ID，如 'deepseek'
  fallbackModels?: string[];  // 备用模型列表
  
  // 提示词
  systemPrompt: string;
  
  // 能力
  capabilities: string[];
  tools?: string[];
  
  // 监督配置
  supervision: SupervisionConfig;
  
  // 元数据
  enabled: boolean;
  createdAt: number;
  updatedAt: number;
}

/**
 * 创建 Agent 的选项
 */
export interface CreateAgentOptions {
  name: string;
  teamId: string;
  kind: AgentKind;
  modelId?: string;
  providerId?: string;
  systemPrompt?: string;
  capabilities?: string[];
  tools?: string[];
  supervision?: Partial<SupervisionConfig>;
}

// ============== Actor 运行时 ==============

/**
 * Actor 实例 - 包装 XState Actor
 */
export interface ActorInstance {
  id: string;
  kind: AgentKind;
  role: AgentRole;
  ref: ActorRefFrom<typeof agentMachine>;  // XState Actor 引用
  children: Set<string>;                  // 子 Actor ID
  metadata: Record<string, unknown>;      // 额外元数据
}

/**
 * 系统 Actor 类型
 */
export type SystemActorType = 'router' | 'logger' | 'persister';

/**
 * Actor 生命周期状态
 */
export type ActorLifecycle = 
  | 'initializing'   // 初始化中
  | 'running'        // 运行中
  | 'stopping'       // 停止中
  | 'stopped'        // 已停止
  | 'restarting'     // 重启中
  | 'failed';        // 失败

/**
 * Actor 系统状态
 */
export type SystemStatus = 
  | 'initializing' 
  | 'running' 
  | 'stopping' 
  | 'stopped';

// ============== 系统 Actor 预定义 ==============

/**
 * 系统 Actor 预定义 - 系统启动时自动创建
 */
export const SYSTEM_AGENTS: AgentDefinition[] = [
  {
    id: 'system-router',
    name: 'System Router',
    teamId: 'system',
    role: 'system',
    kind: 'system',
    modelId: 'deepseek-chat',
    providerId: 'deepseek',
    systemPrompt: 'Router Agent - routes messages to appropriate agents based on content analysis.',
    capabilities: ['routing', 'loadbalancing'],
    supervision: { strategy: 'stop', maxRetries: 0, retryInterval: 0, exponentialBackoff: false, maxBackoff: 0 },
    enabled: true,
    createdAt: 0,
    updatedAt: 0,
  },
  {
    id: 'system-logger',
    name: 'System Logger',
    teamId: 'system',
    role: 'system',
    kind: 'system',
    modelId: 'deepseek-chat',
    providerId: 'deepseek',
    systemPrompt: 'Logger Agent - aggregates and processes logs from all other agents.',
    capabilities: ['logging', 'aggregation'],
    supervision: { strategy: 'stop', maxRetries: 0, retryInterval: 0, exponentialBackoff: false, maxBackoff: 0 },
    enabled: true,
    createdAt: 0,
    updatedAt: 0,
  },
  {
    id: 'system-persister',
    name: 'System Persister',
    teamId: 'system',
    role: 'system',
    kind: 'system',
    modelId: 'deepseek-chat',
    providerId: 'deepseek',
    systemPrompt: 'Persister Agent - manages actor state snapshots and recovery.',
    capabilities: ['persistence', 'snapshot', 'recovery'],
    supervision: { strategy: 'stop', maxRetries: 0, retryInterval: 0, exponentialBackoff: false, maxBackoff: 0 },
    enabled: true,
    createdAt: 0,
    updatedAt: 0,
  },
];

// ============== 事件类型 ==============

/**
 * Actor 系统事件
 */
export type ActorSystemEvent =
  | { type: 'agent:started'; agentId: string; kind: AgentKind }
  | { type: 'agent:stopped'; agentId: string }
  | { type: 'agent:failed'; agentId: string; error: string }
  | { type: 'agent:stateChange'; agentId: string; state: string; context: Record<string, unknown> }
  | { type: 'system:started' }
  | { type: 'system:stopped' }
  | { type: 'snapshot:created'; agentId: string; timestamp: number }
  | { type: 'snapshot:restored'; agentId: string; timestamp: number };

// ============== 错误类型 ==============

/**
 * Actor 系统错误
 */
export class ActorSystemError extends Error {
  constructor(
    message: string,
    public readonly code: ActorErrorCode,
    public readonly agentId?: string,
    public readonly cause?: Error
  ) {
    super(message);
    this.name = 'ActorSystemError';
  }
}

export type ActorErrorCode =
  | 'AGENT_NOT_FOUND'
  | 'AGENT_ALREADY_EXISTS'
  | 'AGENT_RUNNING'
  | 'AGENT_STOPPED'
  | 'FACTORY_NOT_FOUND'
  | 'FACTORY_ALREADY_EXISTS'
  | 'SYSTEM_NOT_RUNNING'
  | 'INVALID_CONFIG'
  | 'CANNOT_DELETE_SYSTEM_AGENT';
