/**
 * ============================================
 * Actor Supervisor 单元测试
 * ============================================
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { Supervisor } from './supervisor.js';
import type { ActorSystem } from './actor-system.js';
import type { ActorInstance, SupervisionConfig } from './types.js';
import { Logger } from '../logger/index.js';

// Mock Logger
const createMockLogger = () => ({
  debug: vi.fn(),
  info: vi.fn(),
  warn: vi.fn(),
  error: vi.fn(),
}) as unknown as Logger;

// Mock ActorInstance
const createMockActor = (
  id: string,
  supervision: Partial<SupervisionConfig> = {}
): ActorInstance => ({
  id,
  kind: 'chat',
  role: 'user',
  ref: {
    send: vi.fn(),
    subscribe: vi.fn(() => () => {}),
    stop: vi.fn(),
    getSnapshot: vi.fn(() => ({ value: 'idle', status: 'done', context: {} })),
  },
  children: new Set(),
  metadata: {
    definition: {
      supervision: {
        strategy: 'one_for_one',
        maxRetries: 3,
        retryInterval: 10,
        exponentialBackoff: false,
        maxBackoff: 1000,
        ...supervision,
      },
    },
  },
});

// Mock ActorSystem
const createMockSystem = (): ActorSystem => ({
  handleAgentFailure: vi.fn(),
  stopAgent: vi.fn().mockResolvedValue(undefined),
  restartAgent: vi.fn().mockResolvedValue({} as ActorInstance),
} as unknown as ActorSystem);

describe('Supervisor', () => {
  let supervisor: Supervisor;
  let mockSystem: ActorSystem;
  let mockLogger: Logger;

  beforeEach(() => {
    vi.clearAllMocks();
    mockSystem = createMockSystem();
    mockLogger = createMockLogger();
    supervisor = new Supervisor(mockSystem, mockLogger);
  });

  describe('watch', () => {
    it('应该开始监督 Actor', () => {
      const actor = createMockActor('actor-1');
      supervisor.watch(actor);

      expect(mockLogger.info).toHaveBeenCalledWith(
        expect.objectContaining({
          category: 'actor',
          message: 'Actor watching started',
        })
      );
    });

    it('重复监督应该警告', () => {
      const actor = createMockActor('actor-1');
      supervisor.watch(actor);
      supervisor.watch(actor); // 重复

      expect(mockLogger.warn).toHaveBeenCalledWith(
        expect.objectContaining({
          message: 'Actor already being watched',
        })
      );
    });

    it('应该订阅 Actor 状态变化', () => {
      const actor = createMockActor('actor-1');
      const subscribeFn = actor.ref.subscribe as ReturnType<typeof vi.fn>;

      supervisor.watch(actor);

      expect(subscribeFn).toHaveBeenCalled();
    });

    it('应该使用默认监督配置', () => {
      const actor = createMockActor('actor-1', {});
      // actor 没有定义 supervision，使用默认值
      actor.metadata.definition = {} as any;

      supervisor.watch(actor);

      const status = supervisor.getWatchStatus();
      expect(status[0].strategy).toBe('one_for_one');
      expect(status[0].restartCount).toBe(0);
    });
  });

  describe('unwatch', () => {
    it('应该停止监督', () => {
      const actor = createMockActor('actor-1');
      supervisor.watch(actor);
      supervisor.unwatch('actor-1');

      expect(mockLogger.info).toHaveBeenCalledWith(
        expect.objectContaining({
          message: 'Actor unwatched',
        })
      );
    });

    it('取消监督不存在的 Actor 应该静默处理', () => {
      expect(() => supervisor.unwatch('non-existent')).not.toThrow();
    });
  });

  describe('监督策略 - stop', () => {
    it('应该立即停止 Actor', async () => {
      const actor = createMockActor('actor-1', { strategy: 'stop', maxRetries: 0 });
      supervisor.watch(actor);

      // 触发错误
      const subscribeFn = (actor.ref.subscribe as ReturnType<typeof vi.fn>).mock.calls[0][0];
      subscribeFn.error('Test error');

      // 等待异步处理
      await new Promise(resolve => setTimeout(resolve, 50));

      expect(mockSystem.stopAgent).toHaveBeenCalledWith('actor-1');
    });
  });

  describe('监督策略 - one_for_one', () => {
    it('应该重启失败的 Actor', async () => {
      const actor = createMockActor('actor-1', {
        strategy: 'one_for_one',
        maxRetries: 3,
        retryInterval: 10,
      });
      supervisor.watch(actor);

      // 触发错误
      const subscribeFn = (actor.ref.subscribe as ReturnType<typeof vi.fn>).mock.calls[0][0];
      subscribeFn.error('Test error');

      // 等待异步处理
      await new Promise(resolve => setTimeout(resolve, 50));

      expect(mockSystem.restartAgent).toHaveBeenCalledWith('actor-1');
    });

    it('超过最大重试次数应该停止', async () => {
      const actor = createMockActor('actor-1', {
        strategy: 'one_for_one',
        maxRetries: 1,
        retryInterval: 10,
      });
      supervisor.watch(actor);

      const subscribeFn = (actor.ref.subscribe as ReturnType<typeof vi.fn>).mock.calls[0][0];

      // 第一次失败 - 重启
      subscribeFn.error('Error 1');
      await new Promise(resolve => setTimeout(resolve, 50));

      // 第二次失败 - 超过最大重试，停止
      subscribeFn.error('Error 2');
      await new Promise(resolve => setTimeout(resolve, 50));

      expect(mockSystem.stopAgent).toHaveBeenCalledWith('actor-1');
    });
  });

  describe('指数退避', () => {
    it('应该使用指数退避计算等待时间', async () => {
      const actor = createMockActor('actor-1', {
        strategy: 'one_for_one',
        maxRetries: 3,
        retryInterval: 10,
        exponentialBackoff: true,
        maxBackoff: 1000,
      });
      supervisor.watch(actor);

      const subscribeFn = (actor.ref.subscribe as ReturnType<typeof vi.fn>).mock.calls[0][0];

      // 第一次
      subscribeFn.error('Error 1');
      let status = supervisor.getWatchStatus();
      expect(status[0].restartCount).toBe(0); // 还未增加

      await new Promise(resolve => setTimeout(resolve, 20));

      status = supervisor.getWatchStatus();
      expect(status[0].restartCount).toBe(1);

      // 第二次 - 应该有更长的等待时间
      subscribeFn.error('Error 2');
      await new Promise(resolve => setTimeout(resolve, 50));

      status = supervisor.getWatchStatus();
      expect(status[0].restartCount).toBe(2);
    });
  });

  describe('getWatchStatus', () => {
    it('应该返回所有监督的 Actor 状态', () => {
      const actor1 = createMockActor('actor-1', { strategy: 'one_for_one' });
      const actor2 = createMockActor('actor-2', { strategy: 'stop' });

      supervisor.watch(actor1);
      supervisor.watch(actor2);

      const status = supervisor.getWatchStatus();

      expect(status).toHaveLength(2);
      expect(status.find(s => s.agentId === 'actor-1')?.strategy).toBe('one_for_one');
      expect(status.find(s => s.agentId === 'actor-2')?.strategy).toBe('stop');
    });

    it('空监督列表应该返回空数组', () => {
      const status = supervisor.getWatchStatus();
      expect(status).toEqual([]);
    });
  });

  describe('状态变化检测', () => {
    it('应该检测错误状态并触发处理', async () => {
      const actor = createMockActor('actor-1', { strategy: 'stop' });
      supervisor.watch(actor);

      const subscribeFn = (actor.ref.subscribe as ReturnType<typeof vi.fn>).mock.calls[0][0];

      // 模拟错误状态
      subscribeFn.next({ status: 'error', value: 'failed', error: { message: 'Failed' } });

      await new Promise(resolve => setTimeout(resolve, 50));

      // 错误状态应该被检测到，然后根据策略停止
      expect(mockLogger.warn).toHaveBeenCalledWith(
        expect.objectContaining({ message: 'Actor failure detected' })
      );
    });

    it('错误订阅应该调用 handleFailure', async () => {
      const actor = createMockActor('actor-1', { strategy: 'stop' });
      supervisor.watch(actor);

      const subscribeFn = (actor.ref.subscribe as ReturnType<typeof vi.fn>).mock.calls[0][0];

      // 模拟错误回调
      subscribeFn.error('Test error');

      await new Promise(resolve => setTimeout(resolve, 50));

      expect(mockLogger.warn).toHaveBeenCalledWith(
        expect.objectContaining({ message: 'Actor failure detected' })
      );
    });
  });
});
