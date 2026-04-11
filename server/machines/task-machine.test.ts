/**
 * ============================================
 * Task Machine 单元测试
 * ============================================
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createActor } from 'xstate';
import {
  taskMachine,
  createTaskActor,
  DEFAULT_TIMEOUT,
  DEFAULT_MAX_RETRIES,
  DEFAULT_PRIORITY,
} from './task-machine.js';

describe('Task Machine', () => {
  describe('初始状态', () => {
    it('初始状态应该是 pending', () => {
      const actor = createActor(taskMachine, {
        input: { taskId: 'task-1', content: 'Test task' },
      });
      actor.start();
      expect(actor.getSnapshot().value).toBe('pending');
      actor.stop();
    });

    it('默认配置应该正确', () => {
      const actor = createActor(taskMachine, {
        input: { taskId: 'task-1', content: 'Test task' },
      });
      actor.start();
      const ctx = actor.getSnapshot().context;

      expect(ctx.taskId).toBe('task-1');
      expect(ctx.content).toBe('Test task');
      expect(ctx.type).toBe('chat');
      expect(ctx.priority).toBe(DEFAULT_PRIORITY);
      expect(ctx.timeout).toBe(DEFAULT_TIMEOUT);
      expect(ctx.maxRetries).toBe(DEFAULT_MAX_RETRIES);
      expect(ctx.retries).toBe(0);
      expect(ctx.assignee).toBeNull();

      actor.stop();
    });
  });

  describe('pending 状态', () => {
    it('收到 start 事件应该转换到 assigned', () => {
      const actor = createActor(taskMachine, {
        input: { taskId: 'task-1', content: 'Test' },
      });
      actor.start();

      actor.send({ type: 'start', assignee: 'agent-1' });

      expect(actor.getSnapshot().value).toBe('assigned');
      expect(actor.getSnapshot().context.assignee).toBe('agent-1');
      actor.stop();
    });

    it('没有依赖的任务可以直接 start', () => {
      const actor = createActor(taskMachine, {
        input: { taskId: 'task-1', content: 'Test', dependencies: [] },
      });
      actor.start();

      actor.send({ type: 'start', assignee: 'agent-1' });

      expect(actor.getSnapshot().value).toBe('assigned');
      actor.stop();
    });

    it('有依赖的任务在依赖未完成时不能 start', () => {
      const actor = createActor(taskMachine, {
        input: { taskId: 'task-1', content: 'Test', dependencies: ['dep-1'] },
      });
      actor.start();

      // 检查 guard 是否阻止了转换
      const snapshot = actor.getSnapshot();
      actor.send({ type: 'start', assignee: 'agent-1' });

      // 由于依赖未完成，状态应该仍然是 pending
      expect(actor.getSnapshot().value).toBe('pending');
      actor.stop();
    });
  });

  describe('assigned 状态', () => {
    it('收到 start 事件应该转换到 running', () => {
      const actor = createActor(taskMachine, {
        input: { taskId: 'task-1', content: 'Test' },
      });
      actor.start();

      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'start', assignee: 'agent-1' });

      expect(actor.getSnapshot().value).toBe('running');
      expect(actor.getSnapshot().context.startTime).not.toBeNull();
      actor.stop();
    });

    it('收到 cancel 事件应该转换到 cancelled', () => {
      const actor = createActor(taskMachine, {
        input: { taskId: 'task-1', content: 'Test' },
      });
      actor.start();

      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'cancel', reason: 'User cancelled' });

      expect(actor.getSnapshot().value).toBe('cancelled');
      actor.stop();
    });
  });

  describe('running 状态', () => {
    it('收到 complete 事件应该转换到 completed', () => {
      const actor = createActor(taskMachine, {
        input: { taskId: 'task-1', content: 'Test' },
      });
      actor.start();

      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'complete', result: { data: 'result' } });

      expect(actor.getSnapshot().value).toBe('completed');
      expect(actor.getSnapshot().context.result).toEqual({ data: 'result' });
      actor.stop();
    });

    it('收到 fail 事件应该转换到 failed', () => {
      const actor = createActor(taskMachine, {
        input: { taskId: 'task-1', content: 'Test' },
      });
      actor.start();

      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'fail', error: 'Something went wrong' });

      expect(actor.getSnapshot().value).toBe('failed');
      expect(actor.getSnapshot().context.error).toBe('Something went wrong');
      actor.stop();
    });

    it('收到 cancel 事件应该转换到 cancelled', () => {
      const actor = createActor(taskMachine, {
        input: { taskId: 'task-1', content: 'Test' },
      });
      actor.start();

      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'cancel' });

      expect(actor.getSnapshot().value).toBe('cancelled');
      actor.stop();
    });

    it('应该记录执行时长', async () => {
      const actor = createActor(taskMachine, {
        input: { taskId: 'task-1', content: 'Test' },
      });
      actor.start();

      // 检查初始状态
      expect(actor.getSnapshot().value).toBe('pending');
      expect(actor.getSnapshot().context.startTime).toBeNull();

      // 进入 assigned 状态
      actor.send({ type: 'start', assignee: 'agent-1' });
      expect(actor.getSnapshot().value).toBe('assigned');
      expect(actor.getSnapshot().context.startTime).not.toBeNull();

      // 进入 running 状态
      actor.send({ type: 'start', assignee: 'agent-1' });
      expect(actor.getSnapshot().value).toBe('running');

      // 等待一小段时间确保 startTime 被设置
      await new Promise(resolve => setTimeout(resolve, 10));

      // 完成时传入 result
      actor.send({ type: 'complete', result: 'done' });

      const ctx = actor.getSnapshot().context;
      expect(actor.getSnapshot().value).toBe('completed');
      expect(ctx.result).toBe('done');
      expect(ctx.duration).not.toBeNull();
      expect(typeof ctx.duration).toBe('number');
      expect(ctx.duration).toBeGreaterThanOrEqual(0);
      actor.stop();
    });
  });

  describe('completed 状态', () => {
    it('completed 是最终状态', () => {
      const actor = createActor(taskMachine, {
        input: { taskId: 'task-1', content: 'Test' },
      });
      actor.start();

      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'complete', result: 'done' });

      expect(actor.getSnapshot().value).toBe('completed');
      actor.stop();
    });
  });

  describe('failed 状态', () => {
    it('收到 retry 事件应该重试', () => {
      const actor = createActor(taskMachine, {
        input: { taskId: 'task-1', content: 'Test' },
      });
      actor.start();

      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'fail', error: 'Error' });

      expect(actor.getSnapshot().value).toBe('failed');
      expect(actor.getSnapshot().context.retries).toBe(0);

      actor.send({ type: 'retry' });

      expect(actor.getSnapshot().value).toBe('running');
      expect(actor.getSnapshot().context.retries).toBe(1);
      expect(actor.getSnapshot().context.error).toBeNull();
      actor.stop();
    });

    it('超过最大重试次数后不能重试', () => {
      const actor = createActor(taskMachine, {
        input: { taskId: 'task-1', content: 'Test', maxRetries: 2 },
      });
      actor.start();

      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'fail', error: 'Error 1' });

      // 重试 1
      actor.send({ type: 'retry' });
      expect(actor.getSnapshot().context.retries).toBe(1);
      
      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'fail', error: 'Error 2' });

      // 重试 2
      actor.send({ type: 'retry' });
      expect(actor.getSnapshot().context.retries).toBe(2);
      
      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'fail', error: 'Error 3' });

      // 第三次失败，达到 maxRetries，状态应该是 failed
      expect(actor.getSnapshot().value).toBe('failed');
      expect(actor.getSnapshot().context.retries).toBe(2);
      actor.stop();
    });

    it('收到 cancel 事件应该转换到 cancelled', () => {
      const actor = createActor(taskMachine, {
        input: { taskId: 'task-1', content: 'Test' },
      });
      actor.start();

      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'fail', error: 'Error' });
      actor.send({ type: 'cancel' });

      expect(actor.getSnapshot().value).toBe('cancelled');
      actor.stop();
    });
  });

  describe('cancelled 状态', () => {
    it('cancelled 是最终状态', () => {
      const actor = createActor(taskMachine, {
        input: { taskId: 'task-1', content: 'Test' },
      });
      actor.start();

      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'cancel' });

      expect(actor.getSnapshot().value).toBe('cancelled');
      actor.stop();
    });
  });

  describe('工厂函数', () => {
    it('createTaskActor 应该创建正确配置的 actor', () => {
      const actor = createTaskActor('task-custom', 'Custom task content', {
        type: 'delegate',
        priority: 8,
        timeout: 60000,
        maxRetries: 5,
        dependencies: ['dep-1', 'dep-2'],
      });

      actor.start();
      const ctx = actor.getSnapshot().context;

      expect(ctx.taskId).toBe('task-custom');
      expect(ctx.content).toBe('Custom task content');
      expect(ctx.type).toBe('delegate');
      expect(ctx.priority).toBe(8);
      expect(ctx.timeout).toBe(60000);
      expect(ctx.maxRetries).toBe(5);
      expect(ctx.dependencies).toEqual(['dep-1', 'dep-2']);

      actor.stop();
    });

    it('createTaskActor 应该使用默认值', () => {
      const actor = createTaskActor('task-default', 'Default content');
      actor.start();
      const ctx = actor.getSnapshot().context;

      expect(ctx.type).toBe('chat');
      expect(ctx.priority).toBe(DEFAULT_PRIORITY);
      expect(ctx.timeout).toBe(DEFAULT_TIMEOUT);
      expect(ctx.maxRetries).toBe(DEFAULT_MAX_RETRIES);
      expect(ctx.dependencies).toEqual([]);

      actor.stop();
    });
  });

  describe('任务类型', () => {
    it('应该支持 chat 类型', () => {
      const actor = createTaskActor('task-1', 'Chat task', { type: 'chat' });
      actor.start();
      expect(actor.getSnapshot().context.type).toBe('chat');
      actor.stop();
    });

    it('应该支持 delegate 类型', () => {
      const actor = createTaskActor('task-1', 'Delegate task', { type: 'delegate' });
      actor.start();
      expect(actor.getSnapshot().context.type).toBe('delegate');
      actor.stop();
    });

    it('应该支持 tool 类型', () => {
      const actor = createTaskActor('task-1', 'Tool task', { type: 'tool' });
      actor.start();
      expect(actor.getSnapshot().context.type).toBe('tool');
      actor.stop();
    });

    it('应该支持 system 类型', () => {
      const actor = createTaskActor('task-1', 'System task', { type: 'system' });
      actor.start();
      expect(actor.getSnapshot().context.type).toBe('system');
      actor.stop();
    });
  });

  describe('优先级', () => {
    it('应该支持最高优先级 10', () => {
      const actor = createTaskActor('task-1', 'High priority', { priority: 10 });
      actor.start();
      const ctx = actor.getSnapshot().context;
      expect(ctx.priority).toBe(10);
      actor.stop();
    });

    it('应该支持最低优先级 1', () => {
      const actor = createTaskActor('task-1', 'Low priority', { priority: 1 });
      actor.start();
      const ctx = actor.getSnapshot().context;
      expect(ctx.priority).toBe(1);
      actor.stop();
    });
  });

  describe('超时功能', () => {
    it('收到 timeout 事件应该转换到 failed', () => {
      const actor = createTaskActor('task-1', 'Test timeout', { timeout: 1000 });
      actor.start();

      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'timeout' });

      expect(actor.getSnapshot().value).toBe('failed');
      expect(actor.getSnapshot().context.error).toBe('Unknown error');
      actor.stop();
    });

    it('超时任务应该有 duration', async () => {
      const actor = createTaskActor('task-1', 'Test timeout', { timeout: 1000 });
      actor.start();

      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'start', assignee: 'agent-1' });

      await new Promise(resolve => setTimeout(resolve, 10));

      actor.send({ type: 'timeout' });

      const ctx = actor.getSnapshot().context;
      expect(ctx.duration).not.toBeNull();
      actor.stop();
    });
  });

  describe('依赖管理', () => {
    it('没有依赖的任务可以直接启动', () => {
      const actor = createTaskActor('task-1', 'No deps', { dependencies: [] });
      actor.start();

      actor.send({ type: 'start', assignee: 'agent-1' });

      expect(actor.getSnapshot().value).toBe('assigned');
      actor.stop();
    });
  });

  describe('失败状态', () => {
    it('失败任务的重试次数应该递增', () => {
      const actor = createTaskActor('task-1', 'Retry test', { maxRetries: 3 });
      actor.start();

      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'fail', error: 'Error 1' });
      expect(actor.getSnapshot().context.retries).toBe(0);

      actor.send({ type: 'retry' });
      actor.send({ type: 'start', assignee: 'agent-1' });
      actor.send({ type: 'fail', error: 'Error 2' });
      expect(actor.getSnapshot().context.retries).toBe(1);

      actor.send({ type: 'retry' });
      expect(actor.getSnapshot().context.retries).toBe(2);
      actor.stop();
    });
  });
});
