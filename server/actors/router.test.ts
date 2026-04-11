/**
 * ============================================
 * Actor Router 单元测试
 * ============================================
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
  Router,
  RoundRobinStrategy,
  LeastLoadStrategy,
  TypeMatchStrategy,
  AffinityStrategy,
} from './router.js';
import type { ActorSystem } from './actor-system.js';
import type { ActorInstance } from './types.js';

// Mock ActorSystem
const createMockActor = (id: string, kind: string = 'chat', role: string = 'user'): ActorInstance => ({
  id,
  kind: kind as any,
  role: role as any,
  ref: {
    send: vi.fn(),
    subscribe: vi.fn(() => () => {}),
    stop: vi.fn(),
    getSnapshot: vi.fn(() => ({ value: 'idle', context: {} })),
  },
  children: new Set(),
  metadata: {},
});

const createMockSystem = (agents: ActorInstance[] = []): ActorSystem => {
  return {
    getAgentsByRole: vi.fn((role: string) => agents.filter(a => a.role === role)),
    getAllAgents: vi.fn(() => agents),
  } as unknown as ActorSystem;
};

describe('Router', () => {
  describe('RoundRobinStrategy', () => {
    it('应该使用 round_robin 名称', () => {
      const strategy = new RoundRobinStrategy();
      expect(strategy.name).toBe('round_robin');
    });

    it('应该循环选择 Agent', () => {
      const strategy = new RoundRobinStrategy();
      const agents = [
        createMockActor('agent-1'),
        createMockActor('agent-2'),
        createMockActor('agent-3'),
      ];

      expect(strategy.select(agents, {} as any)?.id).toBe('agent-1');
      expect(strategy.select(agents, {} as any)?.id).toBe('agent-2');
      expect(strategy.select(agents, {} as any)?.id).toBe('agent-3');
      expect(strategy.select(agents, {} as any)?.id).toBe('agent-1'); // 循环
    });

    it('空数组应该返回 null', () => {
      const strategy = new RoundRobinStrategy();
      expect(strategy.select([], {} as any)).toBeNull();
    });

    it('单个 Agent 应该总是返回该 Agent', () => {
      const strategy = new RoundRobinStrategy();
      const agents = [createMockActor('only-agent')];

      expect(strategy.select(agents, {} as any)?.id).toBe('only-agent');
      expect(strategy.select(agents, {} as any)?.id).toBe('only-agent');
    });
  });

  describe('LeastLoadStrategy', () => {
    it('应该使用 least_load 名称', () => {
      const strategy = new LeastLoadStrategy();
      expect(strategy.name).toBe('least_load');
    });

    it('应该选择负载最低的 Agent', () => {
      const strategy = new LeastLoadStrategy();
      const agents = [
        { ...createMockActor('agent-1'), metadata: { currentTasks: 5 } },
        { ...createMockActor('agent-2'), metadata: { currentTasks: 1 } },
        { ...createMockActor('agent-3'), metadata: { currentTasks: 3 } },
      ];

      expect(strategy.select(agents, {} as any)?.id).toBe('agent-2');
    });

    it('应该处理没有 currentTasks 的情况', () => {
      const strategy = new LeastLoadStrategy();
      const agents = [
        { ...createMockActor('agent-1'), metadata: {} },
        { ...createMockActor('agent-2'), metadata: { currentTasks: 1 } },
      ];

      // 没有 currentTasks 的被视为 0
      expect(strategy.select(agents, {} as any)?.id).toBe('agent-1');
    });
  });

  describe('TypeMatchStrategy', () => {
    it('应该使用 type_match 名称', () => {
      const strategy = new TypeMatchStrategy();
      expect(strategy.name).toBe('type_match');
    });

    it('应该根据内容推断 code 类型', () => {
      const strategy = new TypeMatchStrategy();
      const agents = [
        createMockActor('agent-1', 'chat'),
        createMockActor('agent-2', 'code'),
      ];

      const message = { content: 'Please write a function to calculate fibonacci' } as any;
      expect(strategy.select(agents, message)?.id).toBe('agent-2');
    });

    it('应该根据内容推断 tool 类型', () => {
      const strategy = new TypeMatchStrategy();
      const agents = [
        createMockActor('agent-1', 'chat'),
        createMockActor('agent-2', 'tool'),
      ];

      const message = { content: 'Search for information about AI' } as any;
      expect(strategy.select(agents, message)?.id).toBe('agent-2');
    });

    it('应该根据内容推断 planner 类型', () => {
      const strategy = new TypeMatchStrategy();
      const agents = [
        createMockActor('agent-1', 'chat'),
        createMockActor('agent-2', 'planner'),
      ];

      const message = { content: 'Plan a schedule for next week' } as any;
      expect(strategy.select(agents, message)?.id).toBe('agent-2');
    });

    it('默认应该选择 chat 类型', () => {
      const strategy = new TypeMatchStrategy();
      const agents = [
        createMockActor('agent-1', 'chat'),
        createMockActor('agent-2', 'planner'),
      ];

      const message = { content: 'Hello world' } as any;
      expect(strategy.select(agents, message)?.id).toBe('agent-1');
    });

    it('没有匹配类型时应该回退到第一个 Agent', () => {
      const strategy = new TypeMatchStrategy();
      const agents = [createMockActor('agent-1', 'planner')];

      const message = { content: 'Hello world' } as any;
      expect(strategy.select(agents, message)?.id).toBe('agent-1');
    });
  });

  describe('AffinityStrategy', () => {
    it('应该使用 affinity 名称', () => {
      const strategy = new AffinityStrategy();
      expect(strategy.name).toBe('affinity');
    });

    it('应该记住之前的任务分配', () => {
      const strategy = new AffinityStrategy();
      const agents = [
        createMockActor('agent-1'),
        createMockActor('agent-2'),
      ];

      const message1 = { content: 'Hello world' } as any;
      const message2 = { content: 'Hello world' } as any; // 相同内容

      // 第一次选择 agent-1
      const selected1 = strategy.select(agents, message1);
      expect(selected1?.id).toBe('agent-1');

      // 第二次应该记住选择 agent-1
      const selected2 = strategy.select(agents, message2);
      expect(selected2?.id).toBe('agent-1');
    });

    it('不同内容应该创建新的亲和性', () => {
      const strategy = new AffinityStrategy();
      const agents = [
        createMockActor('agent-1'),
        createMockActor('agent-2'),
      ];

      const message1 = { content: 'Hello' } as any;
      const message2 = { content: 'World' } as any;

      strategy.select(agents, message1); // 选择 agent-1
      const selected2 = strategy.select(agents, message2);

      // 不同内容可能选择不同 agent
      expect(selected2).toBeDefined();
    });
  });

  describe('Router', () => {
    it('应该使用默认的 RoundRobinStrategy', () => {
      const system = createMockSystem([createMockActor('agent-1')]);
      const router = new Router(system);

      expect(router.getStats().strategy).toBe('round_robin');
    });

    it('应该支持设置自定义策略', () => {
      const system = createMockSystem([createMockActor('agent-1')]);
      const router = new Router(system);

      const customStrategy = new LeastLoadStrategy();
      router.setStrategy(customStrategy);

      expect(router.getStats().strategy).toBe('least_load');
    });

    it('没有可用 Agent 时应该返回 null', () => {
      const system = createMockSystem([]);
      const router = new Router(system);

      const message = { taskId: 'task-1', content: 'Hello' } as any;
      expect(router.route(message)).toBeNull();
    });

    it('应该路由消息到选中的 Agent', () => {
      const agent = createMockActor('agent-1');
      const system = createMockSystem([agent]);
      const router = new Router(system);

      const message = { taskId: 'task-1', content: 'Hello', from: 'user' } as any;
      const result = router.route(message);

      expect(result).toBe(agent);
    });

    it('应该只路由用户 Agent', () => {
      const systemAgent = createMockActor('system-agent', 'chat', 'system');
      const userAgent = createMockActor('user-agent', 'chat', 'user');
      const system = createMockSystem([systemAgent, userAgent]);

      const router = new Router(system);
      const message = { taskId: 'task-1', content: 'Hello' } as any;

      const result = router.route(message);

      // 只有用户 Agent 应该被选中
      expect(result?.id).toBe('user-agent');
      expect(result?.role).toBe('user');
    });

    it('应该返回正确的统计信息', () => {
      const agents = [createMockActor('agent-1'), createMockActor('agent-2')];
      const system = createMockSystem(agents);
      const router = new Router(system);

      const stats = router.getStats();
      expect(stats.strategy).toBe('round_robin');
      expect(stats.agentCount).toBe(2);
    });
  });
});
