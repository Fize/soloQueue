/**
 * ============================================
 * Task Machine (任务状态机)
 * ============================================
 *
 * 【职责】
 * - 管理单个任务的生命周期
 * - 支持任务依赖管理
 * - 提供优先级队列
 * - 超时控制和重试机制
 *
 * 【状态图】
 *
 *   pending ──start──▶ assigned ──start──▶ running ──▶ completed
 *       │                                  │
 *       │ cancel                           │ fail/cancel/timeout
 *       ▼                                  ▼
 *   cancelled                           failed
 *       ▲                                  │
 *       │                                  │ retry (≤ max)
 *       └──────────────────────────────────┘
 *
 * 【配置】
 *
 *   - 队列容量: 100 (默认)
 *   - 超时时间: 30 秒 (默认)
 *   - 最大重试: 3 次
 *   - 优先级: 1-10 (10 最高)
 *
 * ============================================
 */

import { setup, assign, fromPromise } from 'xstate';
import { createActor, type ActorRefFrom } from 'xstate';
import { Logger } from '../logger/index.js';

// ============== Types ==============

export type TaskType = 'chat' | 'delegate' | 'tool' | 'system';

export interface TaskContext {
  // 任务信息
  taskId: string;
  type: TaskType;
  content: string;

  // 执行信息
  assignee: string | null;
  startTime: number | null;
  endTime: number | null;
  duration: number | null;

  // 结果
  result: unknown | null;
  error: string | null;

  // 配置
  priority: number;             // 优先级 1-10 (10 最高)
  retries: number;             // 当前重试次数
  maxRetries: number;         // 最大重试次数
  timeout: number;            // 超时时间 (ms)

  // 依赖
  dependencies: string[];
  blockingTasks: string[];     // 被当前任务阻塞的任务
}

export interface TaskInput {
  taskId: string;
  type?: TaskType;
  content: string;
  priority?: number;
  timeout?: number;
  maxRetries?: number;
  dependencies?: string[];
}

export type TaskEvent =
  | { type: 'start'; assignee: string }
  | { type: 'complete'; result: unknown }
  | { type: 'fail'; error: string }
  | { type: 'cancel'; reason?: string }
  | { type: 'retry' }
  | { type: 'timeout' }
  | { type: 'dependency.complete'; taskId: string }
  | { type: 'dependency.fail'; taskId: string };

// ============== Constants ==============

const DEFAULT_TIMEOUT = 30 * 1000;              // 30 秒
const DEFAULT_MAX_RETRIES = 3;
const DEFAULT_PRIORITY = 5;

// ============== Logger ==============

const logger = Logger.contextualize('[Task Machine]');

// ============== Guards ==============

const guards = {
  canRetry: ({ context }: { context: TaskContext }) =>
    context.retries < context.maxRetries,

  hasDependencies: ({ context }: { context: TaskContext }) =>
    context.dependencies.length > 0,

  allDependenciesComplete: ({ context }: { context: TaskContext }) => {
    // 此检查在实际使用时由 TaskQueue 完成
    return context.dependencies.length === 0;
  },

  isTimeout: ({ context }: { context: TaskContext }) => {
    if (!context.timeout || !context.startTime) return false;
    return Date.now() - context.startTime > context.timeout;
  },
};

// ============== Actions ==============

const actions = {
  // 开始执行
  start: assign(({ context, event }: { context: TaskContext; event: any }) => ({
    assignee: event.assignee || context.assignee,
    startTime: Date.now(),
    endTime: null,
    duration: null,
    error: null,
  })),

  // 完成任务
  complete: assign(({ context }: { context: TaskContext }) => ({
    result: context.result,
    endTime: Date.now(),
    duration: context.startTime ? Date.now() - context.startTime : null,
  })),

  // 任务失败
  fail: assign(({ context, event }: { context: TaskContext; event: any }) => ({
    error: event.error || 'Unknown error',
    endTime: Date.now(),
    duration: context.startTime ? Date.now() - context.startTime : null,
  })),

  // 重试
  retry: assign(({ context }: { context: TaskContext }) => ({
    retries: context.retries + 1,
    startTime: Date.now(),
    endTime: null,
    duration: null,
    error: null,
  })),

  // 取消
  cancel: assign(({ context }: { context: TaskContext }) => ({
    endTime: Date.now(),
    duration: context.startTime ? Date.now() - context.startTime : null,
  })),

  // 日志
  logStart: ({ context }: { context: TaskContext }) => {
    logger.info('Task started', {
      taskId: context.taskId,
      type: context.type,
      priority: context.priority,
      timeout: context.timeout,
    });
  },

  logComplete: ({ context }: { context: TaskContext }) => {
    logger.info('Task completed', {
      taskId: context.taskId,
      duration: context.duration,
      resultSize: JSON.stringify(context.result || '').length,
    });
  },

  logFail: ({ context }: { context: TaskContext }) => {
    logger.error('Task failed', {
      taskId: context.taskId,
      error: context.error,
      retries: context.retries,
    });
  },

  logTimeout: ({ context }: { context: TaskContext }) => {
    logger.warn('Task timeout', {
      taskId: context.taskId,
      duration: context.duration,
    });
  },
};

// ============== Machine ==============

export const taskMachine = setup({
  types: {
    context: {} as TaskContext,
    events: {} as TaskEvent,
  },
  actions,
  guards,
}).createMachine({
  id: 'task',
  initial: 'pending',
  createdAt: new Date().toISOString(),

  context: {
    taskId: '',
    type: 'chat',
    content: '',
    assignee: null,
    startTime: null,
    endTime: null,
    duration: null,
    result: null,
    error: null,
    priority: DEFAULT_PRIORITY,
    retries: 0,
    maxRetries: DEFAULT_MAX_RETRIES,
    timeout: DEFAULT_TIMEOUT,
    dependencies: [],
    blockingTasks: [],
  },

  states: {
    // ========== pending ==========
    pending: {
      on: {
        start: {
          target: 'assigned',
          guard: 'allDependenciesComplete',
          actions: 'start',
        },
      },
    },

    // ========== assigned ==========
    assigned: {
      on: {
        start: {
          target: 'running',
          actions: 'start',
        },
        cancel: {
          target: 'cancelled',
          actions: ['cancel'],
        },
      },
    },

    // ========== running ==========
    running: {
      entry: 'logStart',
      on: {
        complete: {
          target: 'completed',
          actions: assign({ result: ({ event }: { event: any }) => event.result }),
        },
        fail: {
          target: 'failed',
          actions: 'fail',
        },
        cancel: {
          target: 'cancelled',
          actions: 'cancel',
        },
        timeout: {
          target: 'failed',
          actions: ['fail', 'logTimeout'],
        },
      },
    },

    // ========== completed ==========
    completed: {
      entry: 'logComplete',
      type: 'final',
    },

    // ========== failed ==========
    failed: {
      entry: 'logFail',
      on: {
        retry: {
          target: 'running',
          actions: 'retry',
          guard: 'canRetry',
        },
        cancel: {
          target: 'cancelled',
        },
      },
    },

    // ========== cancelled ==========
    cancelled: {
      type: 'final',
    },
  },
});

// ============== Task Queue Machine ==============

export interface TaskQueueContext {
  // 任务存储
  tasks: Map<string, ActorRefFrom<typeof taskMachine>>;

  // 队列 (按优先级排序)
  pendingQueue: string[];    // 待执行的任务 ID
  runningTasks: string[];   // 正在执行的任务 ID

  // 配置
  maxQueueSize: number;      // 队列最大容量
  maxRunning: number;       // 最大并发数

  // 统计
  totalProcessed: number;
  totalFailed: number;
}

export type TaskQueueEvent =
  | { type: 'submit'; task: TaskInput }
  | { type: 'task.complete'; taskId: string }
  | { type: 'task.fail'; taskId: string; error: string }
  | { type: 'cancel'; taskId: string }
  | { type: 'drain' }
  | { type: 'resize'; maxRunning: number };

// ============== Task Queue Guards ==============

const queueGuards = {
  queueNotFull: ({ context }: { context: TaskQueueContext }) =>
    context.pendingQueue.length < context.maxQueueSize,

  canStartMore: ({ context }: { context: TaskQueueContext }) =>
    context.runningTasks.length < context.maxRunning,

  taskExists: ({ context, event }: { context: TaskQueueContext; event: any }) =>
    context.tasks.has(event.task?.taskId || ''),
};

// ============== Task Queue Actions ==============

const queueActions = {
  addTask: assign(({ context, event }: { context: TaskQueueContext; event: any }) => {
    const taskInput = event.task;
    const taskId = taskInput.taskId;

    // 创建 task actor
    const taskActor = createActor(taskMachine, {
      input: {
        taskId: taskId,
        type: taskInput.type || 'chat',
        content: taskInput.content,
        priority: taskInput.priority || DEFAULT_PRIORITY,
        timeout: taskInput.timeout || DEFAULT_TIMEOUT,
        maxRetries: taskInput.maxRetries || DEFAULT_MAX_RETRIES,
        dependencies: taskInput.dependencies || [],
      },
    });

    // 启动 actor
    taskActor.start();

    const newTasks = new Map(context.tasks);
    newTasks.set(taskId, taskActor as any);

    // 按优先级插入队列 (数字越大优先级越高)
    const newQueue = [...context.pendingQueue, taskId].sort((a, b) => {
      const taskA = newTasks.get(a);
      const taskB = newTasks.get(b);
      const priorityA = (taskA?.getSnapshot()?.context?.priority || 5) as number;
      const priorityB = (taskB?.getSnapshot()?.context?.priority || 5) as number;
      return priorityB - priorityA; // 高优先级在前
    });

    logger.info('Task submitted', {
      taskId,
      queueSize: newQueue.length,
    });

    return {
      tasks: newTasks,
      pendingQueue: newQueue,
    };
  }),

  startTask: assign(({ context }: { context: TaskQueueContext }) => {
    // 找到最高优先级的任务并启动
    const availableTasks = context.pendingQueue.filter(id => {
      const task = context.tasks.get(id);
      if (!task) return false;

      // 检查依赖是否都完成
      const deps = task.getSnapshot()?.context?.dependencies || [];
      return deps.every((depId: string) => {
        const depTask = context.tasks.get(depId);
        return depTask?.getSnapshot()?.value === 'completed';
      });
    });

    if (availableTasks.length === 0) {
      return { runningTasks: context.runningTasks };
    }

    const taskId = availableTasks[0];
    const task = context.tasks.get(taskId);

    if (task) {
      task.send({ type: 'start', assignee: 'system' });
    }

    return {
      pendingQueue: context.pendingQueue.filter(id => id !== taskId),
      runningTasks: [...context.runningTasks, taskId],
    };
  }),

  removeTask: assign(({ context, event }: { context: TaskQueueContext; event: any }) => {
    const taskId = event.taskId || event;
    const task = context.tasks.get(taskId);

    // 停止 actor
    if (task) {
      task.stop();
    }

    const newTasks = new Map(context.tasks);
    newTasks.delete(taskId);

    logger.info('Task removed', { taskId });

    return {
      tasks: newTasks,
      runningTasks: context.runningTasks.filter(id => id !== taskId),
      pendingQueue: context.pendingQueue.filter(id => id !== taskId),
    };
  }),

  taskComplete: assign(({ context, event }: { context: TaskQueueContext; event: any }) => ({
    runningTasks: context.runningTasks.filter(id => id !== event.taskId),
    totalProcessed: context.totalProcessed + 1,
  })),

  taskFail: assign(({ context, event }: { context: TaskQueueContext; event: any }) => ({
    runningTasks: context.runningTasks.filter(id => id !== event.taskId),
    totalFailed: context.totalFailed + 1,
  })),

  updateMaxRunning: assign(({ context, event }: { context: TaskQueueContext; event: any }) => ({
    maxRunning: event.maxRunning || context.maxRunning,
  })),
};

// ============== Task Queue Machine ==============

export const taskQueueMachine = setup({
  types: {
    context: {} as TaskQueueContext,
    events: {} as TaskQueueEvent,
  },
  actions: queueActions,
  guards: queueGuards,
}).createMachine({
  id: 'taskQueue',
  initial: 'running',
  createdAt: new Date().toISOString(),

  context: {
    tasks: new Map(),
    pendingQueue: [],
    runningTasks: [],
    maxQueueSize: 100,        // 默认队列容量 100
    maxRunning: 10,          // 最大并发 10
    totalProcessed: 0,
    totalFailed: 0,
  },

  states: {
    running: {
      on: {
        submit: {
          guard: 'queueNotFull',
          actions: ['addTask'],
        },
        'task.complete': {
          actions: ['taskComplete', 'startTask'],
        },
        'task.fail': {
          actions: ['taskFail', 'startTask'],
        },
        cancel: {
          actions: 'removeTask',
        },
        drain: {
          // 等待所有任务完成
        },
        resize: {
          actions: 'updateMaxRunning',
        },
      },

      // 自动启动等待中的任务
      always: {
        guard: 'canStartMore',
        actions: 'startTask',
      },
    },
  },
});

// ============== Factory ==============

export interface CreateTaskOptions {
  type?: TaskType;
  priority?: number;
  timeout?: number;
  maxRetries?: number;
  dependencies?: string[];
}

/**
 * 创建 Task Machine Actor
 */
export function createTaskActor(taskId: string, content: string, options: CreateTaskOptions = {}) {
  return createActor(taskMachine, {
    input: {
      taskId,
      content,
      type: options.type || 'chat',
      priority: options.priority || DEFAULT_PRIORITY,
      timeout: options.timeout || DEFAULT_TIMEOUT,
      maxRetries: options.maxRetries || DEFAULT_MAX_RETRIES,
      dependencies: options.dependencies || [],
    },
  });
}

/**
 * 创建 Task Queue Actor
 */
export function createTaskQueueActor(maxQueueSize = 100, maxRunning = 10) {
  return createActor(taskQueueMachine, {
    input: {
      maxQueueSize,
      maxRunning,
    },
  });
}

// ============== Exports ==============

export {
  DEFAULT_TIMEOUT,
  DEFAULT_MAX_RETRIES,
  DEFAULT_PRIORITY,
};
