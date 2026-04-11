/**
 * ============================================
 * Actor System 单元测试
 * ============================================
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { ActorSystem } from './actor-system.js';
import type { AgentFactory } from './extensions.js';
import type { ActorInstance, AgentDefinition } from './types.js';

// 使用 vi.hoisted() 确保 mock 在正确的作用域
const { mockLogger, mockAgentService } = vi.hoisted(() => {
  const mockLogger = {
    debug: vi.fn(),
    info: vi.fn(),
    warn: vi.fn(),
    error: vi.fn(),
  };

  const mockAgentService = {
    create: vi.fn().mockResolvedValue({}),
    findByRole: vi.fn().mockResolvedValue([]),
    updateStatus: vi.fn().mockResolvedValue({}),
    delete: vi.fn().mockResolvedValue({}),
  };

  return { mockLogger, mockAgentService };
});

// Mock 模块依赖
vi.mock('../storage/agent.service.js', () => ({
  AgentService: vi.fn(() => mockAgentService),
}));

vi.mock('../logger/index.js', () => ({
  Logger: {
    system: vi.fn(() => mockLogger),
  },
}));

// Mock llmConfigService
vi.mock('../llm/index.js', () => ({
  llmConfigService: {
    getRoleDefaultConfig: vi.fn((role: string) => ({
      providerId: 'deepseek',
      modelId: role === 'code' ? 'deepseek-coder' : 'deepseek-chat',
      temperature: role === 'code' ? 0.2 : 0.7,
      maxTokens: role === 'code' ? 8192 : 4096,
      contextWindowRatio: role === 'code' ? 0.5 : 0.75,
    })),
    getSupervisorDefaults: vi.fn(() => ({
      defaultStrategy: 'one_for_one',
      maxRetries: 3,
      retryInterval: 1000,
      exponentialBackoff: true,
      maxBackoff: 30000,
    })),
  },
}));

// 创建测试工厂
const createTestFactory = (kind: string = 'chat'): AgentFactory => ({
  kind: kind as any,
  create: (definition: AgentDefinition, system: any): ActorInstance => {
    return {
      id: definition.id,
      kind: definition.kind,
      role: definition.role,
      ref: {
        send: vi.fn(),
        subscribe: vi.fn(() => () => {}),
        stop: vi.fn(),
        getSnapshot: vi.fn(() => ({ value: 'idle', context: {} })),
      },
      children: new Set(),
      metadata: { definition },
    };
  },
  validate: (config: Partial<AgentDefinition>) => {
    return !!config.name && !!config.teamId;
  },
});

// 创建会失败的工厂
const createFailingFactory = (): AgentFactory => ({
  kind: 'failing',
  create: (definition: AgentDefinition, system: any): ActorInstance => {
    throw new Error('Factory create failed');
  },
  validate: () => true,
});

// 创建验证失败的工厂
const createInvalidFactory = (): AgentFactory => ({
  kind: 'invalid',
  create: () => {
    throw new Error('Should not be called');
  },
  validate: () => false,
});

describe('ActorSystem', () => {
  let system: ActorSystem;

  beforeEach(() => {
    vi.clearAllMocks();
    mockAgentService.findByRole.mockResolvedValue([]);
    system = new ActorSystem();
  });

  afterEach(async () => {
    if (system) {
      try {
        await system.stop();
      } catch {}
    }
  });

  describe('构造函数', () => {
    it('应该创建 ActorSystem 实例', () => {
      expect(system).toBeInstanceOf(ActorSystem);
    });

    it('初始状态应该是 initializing', () => {
      expect(system.getStatus().status).toBe('initializing');
    });

    it('初始统计应该为零', () => {
      const stats = system.getStatus().stats;
      expect(stats.createdAgents).toBe(0);
      expect(stats.stoppedAgents).toBe(0);
      expect(stats.failedAgents).toBe(0);
      expect(stats.totalMessages).toBe(0);
    });

    it('应该初始化 Logger', () => {
      expect(mockLogger.info).toHaveBeenCalledWith(
        expect.objectContaining({ message: 'ActorSystem instance created' })
      );
    });
  });

  describe('生命周期', () => {
    it('应该启动系统', async () => {
      await system.start();
      expect(system.getStatus().status).toBe('running');
    });

    it('应该优雅停止系统', async () => {
      await system.start();
      await system.stop();
      expect(system.getStatus().status).toBe('stopped');
    });

    it('重复启动应该警告', async () => {
      await system.start();
      await system.start(); // 重复启动
      expect(system.getStatus().status).toBe('running');
      expect(mockLogger.warn).toHaveBeenCalledWith(
        expect.objectContaining({ message: 'ActorSystem already started' })
      );
    });

    it('停止已停止的系统应该无操作', async () => {
      await system.start();
      await system.stop();
      await system.stop(); // 重复停止
      expect(system.getStatus().status).toBe('stopped');
    });

    it('启动时应该记录日志', async () => {
      await system.start();
      expect(mockLogger.info).toHaveBeenCalledWith(
        expect.objectContaining({ message: 'ActorSystem starting' })
      );
      expect(mockLogger.info).toHaveBeenCalledWith(
        expect.objectContaining({ message: 'ActorSystem started successfully' })
      );
    });
  });

  describe('Agent 管理', () => {
    beforeEach(async () => {
      system.registerFactory(createTestFactory());
      await system.start();
    });

    it('应该创建 Agent', async () => {
      const instance = await system.createAgent({
        name: 'Test Agent',
        teamId: 'team-1',
        kind: 'chat',
      });

      expect(instance).toBeDefined();
      expect(instance.id).toBeDefined();
      expect(instance.kind).toBe('chat');
      expect(system.getAgent(instance.id)).toBe(instance);
    });

    it('应该持久化 Agent 到数据库', async () => {
      await system.createAgent({
        name: 'Test Agent',
        teamId: 'team-1',
        kind: 'chat',
      });

      expect(mockAgentService.create).toHaveBeenCalled();
    });

    it('应该触发 agent:started 事件', async () => {
      const startedHandler = vi.fn();
      system.on('agent:started', startedHandler);

      await system.createAgent({
        name: 'Test Agent',
        teamId: 'team-1',
        kind: 'chat',
      });

      expect(startedHandler).toHaveBeenCalled();
    });

    it('应该阻止创建重复的 Agent', async () => {
      const instance = await system.createAgent({
        id: 'unique-agent',
        name: 'Test Agent',
        teamId: 'team-1',
        kind: 'chat',
      });

      await expect(
        system.createAgent({
          id: 'unique-agent', // 相同 ID
          name: 'Another Agent',
          teamId: 'team-1',
          kind: 'chat',
        })
      ).rejects.toThrow('Agent already exists');
    });

    it('应该触发 agent:stopped 事件', async () => {
      const instance = await system.createAgent({
        name: 'Test Agent',
        teamId: 'team-1',
        kind: 'chat',
      });

      const stoppedHandler = vi.fn();
      system.on('agent:stopped', stoppedHandler);

      await system.stopAgent(instance.id);

      expect(stoppedHandler).toHaveBeenCalled();
    });

    it('应该删除用户 Agent', async () => {
      const instance = await system.createAgent({
        name: 'Test Agent',
        teamId: 'team-1',
        kind: 'chat',
      });

      await system.deleteAgent(instance.id);

      expect(system.getAgent(instance.id)).toBeUndefined();
      expect(mockAgentService.delete).toHaveBeenCalledWith(instance.id);
    });

    it('删除不存在的 Agent 应该抛出错误', async () => {
      await expect(system.deleteAgent('non-existent')).rejects.toThrow('Agent not found');
    });

    it('应该更新统计', async () => {
      await system.createAgent({ name: 'Agent 1', teamId: 'team-1', kind: 'chat' });
      await system.createAgent({ name: 'Agent 2', teamId: 'team-1', kind: 'chat' });

      expect(system.getStatus().stats.createdAgents).toBe(2);
    });

    it('Agent 应该有 user 角色', async () => {
      const instance = await system.createAgent({
        name: 'Test Agent',
        teamId: 'team-1',
        kind: 'chat',
      });

      expect(instance.role).toBe('user');
    });

    it('创建 Agent 失败时应该记录日志', async () => {
      system.registerFactory(createFailingFactory());

      await expect(
        system.createAgent({ name: 'Fail', teamId: 'team-1', kind: 'failing' })
      ).rejects.toThrow('Factory create failed');
    });

    it('工厂验证失败应该抛出错误', async () => {
      system.registerFactory(createInvalidFactory());

      await expect(
        system.createAgent({ name: 'Test', teamId: 'team-1', kind: 'invalid' })
      ).rejects.toThrow('Invalid configuration');
    });

    it('系统未运行时创建 Agent 应该抛出错误', async () => {
      const newSystem = new ActorSystem();
      // 不启动系统
      await expect(
        newSystem.createAgent({ name: 'Test', teamId: 'team-1', kind: 'chat' })
      ).rejects.toThrow('ActorSystem is not running');
    });

    it('未知类型的 Agent 应该抛出错误', async () => {
      await expect(
        system.createAgent({ name: 'Test', teamId: 'team-1', kind: 'unknown' })
      ).rejects.toThrow('No factory for agent kind');
    });

    it('不应该删除系统 Agent', async () => {
      // 创建一个系统 Agent
      const instance = await system.createAgent({
        name: 'System Agent',
        teamId: 'team-1',
        kind: 'chat',
      });
      // 手动修改为系统角色
      instance.role = 'system';

      await expect(system.deleteAgent(instance.id)).rejects.toThrow('Cannot delete system agent');
    });

    it('Agent 状态变化应该触发事件', async () => {
      const stateChangeHandler = vi.fn();
      system.on('agent:stateChange', stateChangeHandler);

      const instance = await system.createAgent({
        name: 'Test Agent',
        teamId: 'team-1',
        kind: 'chat',
      });

      // 触发订阅回调
      const subscribeCallback = instance.ref.subscribe.mock.calls[0][0];
      subscribeCallback({ value: 'working', context: {} });

      expect(stateChangeHandler).toHaveBeenCalled();
    });
  });

  describe('查询', () => {
    beforeEach(async () => {
      system.registerFactory(createTestFactory('chat'));
      system.registerFactory(createTestFactory('code'));
      await system.start();

      await system.createAgent({ name: 'Chat Agent', teamId: 'team-1', kind: 'chat' });
      await system.createAgent({ name: 'Code Agent', teamId: 'team-1', kind: 'code' });
    });

    it('getAgent 应该返回指定 Agent', () => {
      const agents = system.getAllAgents();
      const agent = system.getAgent(agents[0].id);
      expect(agent).toBe(agents[0]);
    });

    it('getAgent 应该对不存在返回 undefined', () => {
      expect(system.getAgent('non-existent')).toBeUndefined();
    });

    it('getAllAgents 应该返回所有 Agent', () => {
      expect(system.getAllAgents().length).toBe(2);
    });

    it('getAgentsByKind 应该按类型过滤', () => {
      const codeAgents = system.getAgentsByKind('code');
      expect(codeAgents.length).toBe(1);
      expect(codeAgents[0].kind).toBe('code');
    });

    it('getAgentsByRole 应该按角色过滤', () => {
      const userAgents = system.getAgentsByRole('user');
      expect(userAgents.every(a => a.role === 'user')).toBe(true);
    });

    it('getAgentsByTeam 应该按团队过滤', async () => {
      await system.createAgent({ name: 'Team 2 Agent', teamId: 'team-2', kind: 'chat' });
      const team1Agents = system.getAgentsByTeam('team-1');
      expect(team1Agents.length).toBe(2);
    });

    it('空查询应该返回空数组', () => {
      const emptyKind = system.getAgentsByKind('planner');
      expect(emptyKind).toEqual([]);
    });
  });

  describe('工厂注册', () => {
    it('应该注册工厂', () => {
      const factory = createTestFactory('custom');
      system.registerFactory(factory);
      // 工厂应该可以用于创建
    });

    it('重复注册应该抛出错误', () => {
      const factory = createTestFactory('chat');
      system.registerFactory(factory);

      expect(() => system.registerFactory(factory)).toThrow('Factory already registered');
    });

    it('注册工厂应该记录日志', () => {
      const factory = createTestFactory('new-kind');
      system.registerFactory(factory);

      expect(mockLogger.info).toHaveBeenCalledWith(
        expect.objectContaining({ message: 'Agent factory registered' })
      );
    });
  });

  describe('消息传递', () => {
    beforeEach(async () => {
      system.registerFactory(createTestFactory());
      await system.start();

      await system.createAgent({ name: 'Test Agent', teamId: 'team-1', kind: 'chat' });
    });

    it('dispatch 应该发送消息', () => {
      const agent = system.getAllAgents()[0];

      system.dispatch({
        type: 'result',
        taskId: 'task-1',
        content: 'Hello',
        from: 'test',
        to: agent.id,
      });

      expect(agent.ref.send).toHaveBeenCalled();
    });

    it('dispatch 应该增加消息计数', () => {
      const before = system.getStatus().stats.totalMessages;

      system.dispatch({
        type: 'result',
        taskId: 'task-1',
        content: 'Hello',
        from: 'test',
        to: 'some-agent',
      });

      expect(system.getStatus().stats.totalMessages).toBe(before + 1);
    });

    it('dispatch 到不存在目标应该警告', () => {
      system.dispatch({
        type: 'result',
        taskId: 'task-1',
        content: 'Hello',
        from: 'test',
        to: 'non-existent-agent',
      });

      expect(mockLogger.warn).toHaveBeenCalledWith(
        expect.objectContaining({ message: 'Message target not found' })
      );
    });

    it('broadcast 应该发送到所有 Agent', async () => {
      // 创建一个新的系统实例
      const broadcastSystem = new ActorSystem();
      broadcastSystem.registerFactory(createTestFactory());
      await broadcastSystem.start();

      await broadcastSystem.createAgent({ name: 'Agent 1', teamId: 'team-1', kind: 'chat' });
      await broadcastSystem.createAgent({ name: 'Agent 2', teamId: 'team-1', kind: 'chat' });

      const agents = broadcastSystem.getAllAgents();

      broadcastSystem.broadcast({
        type: 'system',
        action: 'ping',
      });

      for (const agent of agents) {
        expect(agent.ref.send).toHaveBeenCalled();
      }

      await broadcastSystem.stop();
    });

    it('broadcast 失败应该记录错误', async () => {
      // 创建一个会抛出异常的 Agent
      const failingAgentFactory: AgentFactory = {
        kind: 'failing',
        create: (def, sys) => ({
          id: def.id,
          kind: def.kind,
          role: def.role,
          ref: {
            send: vi.fn().mockImplementation(() => {
              throw new Error('Send failed');
            }),
            subscribe: vi.fn(() => () => {}),
            stop: vi.fn(),
            getSnapshot: vi.fn(() => ({ value: 'idle', context: {} })),
          },
          children: new Set(),
          metadata: { definition: def },
        }),
        validate: () => true,
      };

      const testSystem = new ActorSystem();
      testSystem.registerFactory(failingAgentFactory);
      await testSystem.start();
      await testSystem.createAgent({ name: 'Failing', teamId: 'team-1', kind: 'failing' });

      testSystem.broadcast({ type: 'system', action: 'ping' });

      expect(mockLogger.error).toHaveBeenCalledWith(
        expect.objectContaining({ message: 'Broadcast failed for agent' })
      );

      await testSystem.stop();
    });

    it('ask 应该等待响应', async () => {
      const agent = system.getAllAgents()[0];

      // 模拟响应
      system.on('result', (result) => {
        // 模拟收到响应
      });

      // 注意：这个测试只验证 ask 方法不抛错
      // 完整测试需要更复杂的模拟
      const promise = system.ask({
        type: 'task',
        taskId: 'test-task',
        content: 'Hello',
        from: 'test',
      });

      // 超时取消
      await new Promise(resolve => setTimeout(resolve, 50));
    });

    it('ask 超时应该抛出错误', async () => {
      await expect(
        system.ask({ type: 'task', taskId: 't', content: 'x', from: 'y' }, 10)
      ).rejects.toThrow('Request timeout');
    });
  });

  describe('失败处理', () => {
    beforeEach(async () => {
      system.registerFactory(createTestFactory());
      await system.start();
    });

    it('handleAgentFailure 应该更新统计', async () => {
      await system.createAgent({ name: 'Test', teamId: 'team-1', kind: 'chat' });

      system.handleAgentFailure('test-id', 'Test error');

      expect(system.getStatus().stats.failedAgents).toBe(1);
    });

    it('应该触发 agent:failed 事件', async () => {
      const failedHandler = vi.fn();
      system.on('agent:failed', failedHandler);

      system.handleAgentFailure('test-id', 'Test error');

      expect(failedHandler).toHaveBeenCalled();
    });

    it('多次失败应该累积计数', () => {
      system.handleAgentFailure('test-1', 'Error 1');
      system.handleAgentFailure('test-2', 'Error 2');

      expect(system.getStatus().stats.failedAgents).toBe(2);
    });

    it('失败处理应该记录错误日志', async () => {
      await system.createAgent({ name: 'Test', teamId: 'team-1', kind: 'chat' });

      system.handleAgentFailure('test-id', 'Test error');

      expect(mockLogger.error).toHaveBeenCalledWith(
        expect.objectContaining({ message: 'Agent failure' })
      );
    });
  });

  describe('恢复逻辑', () => {
    it('应该从数据库恢复用户 Agent', async () => {
      const savedAgent: AgentDefinition = {
        id: 'restored-agent',
        name: 'Restored Agent',
        teamId: 'team-1',
        role: 'user',
        kind: 'chat',
        modelId: 'deepseek-chat',
        providerId: 'deepseek',
        systemPrompt: '',
        capabilities: [],
        supervision: { strategy: 'one_for_one', maxRetries: 3 },
        enabled: true,
        createdAt: Date.now(),
        updatedAt: Date.now(),
      };

      mockAgentService.findByRole.mockResolvedValue([savedAgent]);

      system.registerFactory(createTestFactory());
      await system.start();

      const restored = system.getAgent('restored-agent');
      expect(restored).toBeDefined();
      expect(restored?.metadata.restored).toBe(true);
    });

    it('应该跳过禁用的 Agent', async () => {
      const savedAgent: AgentDefinition = {
        id: 'disabled-agent',
        name: 'Disabled Agent',
        teamId: 'team-1',
        role: 'user',
        kind: 'chat',
        modelId: 'deepseek-chat',
        providerId: 'deepseek',
        systemPrompt: '',
        capabilities: [],
        supervision: { strategy: 'one_for_one', maxRetries: 3 },
        enabled: false, // 禁用
        createdAt: Date.now(),
        updatedAt: Date.now(),
      };

      mockAgentService.findByRole.mockResolvedValue([savedAgent]);

      system.registerFactory(createTestFactory());
      await system.start();

      const restored = system.getAgent('disabled-agent');
      expect(restored).toBeUndefined();
    });

    it('恢复失败应该记录日志但不中断', async () => {
      system.registerFactory(createFailingFactory());

      const savedAgent: AgentDefinition = {
        id: 'failing-agent',
        name: 'Failing Agent',
        teamId: 'team-1',
        role: 'user',
        kind: 'failing',
        modelId: 'deepseek-chat',
        providerId: 'deepseek',
        systemPrompt: '',
        capabilities: [],
        supervision: { strategy: 'one_for_one', maxRetries: 3 },
        enabled: true,
        createdAt: Date.now(),
        updatedAt: Date.now(),
      };

      mockAgentService.findByRole.mockResolvedValue([savedAgent]);

      // 应该不抛出错误
      await expect(system.start()).resolves.not.toThrow();
      expect(system.getAgent('failing-agent')).toBeUndefined();
    });
  });

  describe('路由策略', () => {
    beforeEach(async () => {
      system.registerFactory(createTestFactory());
      await system.start();
    });

    it('应该支持设置路由策略', async () => {
      system.registerFactory(createTestFactory('code'));

      system.setRoutingStrategy({
        name: 'test-strategy',
        select: (agents) => agents[0] || null,
      });

      expect(mockLogger.info).toHaveBeenCalledWith(
        expect.objectContaining({ message: 'Routing strategy changed' })
      );
    });
  });

  describe('并发操作', () => {
    it('应该并发创建多个 Agent', async () => {
      system.registerFactory(createTestFactory());
      await system.start();

      const promises = Array.from({ length: 5 }, (_, i) =>
        system.createAgent({ name: `Agent ${i}`, teamId: 'team-1', kind: 'chat' })
      );

      const agents = await Promise.all(promises);

      expect(agents.length).toBe(5);
      expect(system.getAllAgents().length).toBe(5);
    });

    it('应该并发停止多个 Agent', async () => {
      system.registerFactory(createTestFactory());
      await system.start();

      const agents = await Promise.all([
        system.createAgent({ name: 'Agent 1', teamId: 'team-1', kind: 'chat' }),
        system.createAgent({ name: 'Agent 2', teamId: 'team-1', kind: 'chat' }),
        system.createAgent({ name: 'Agent 3', teamId: 'team-1', kind: 'chat' }),
      ]);

      const agentIds = agents.map(a => a.id);

      const stopPromises = agentIds.map(id => system.stopAgent(id));
      await Promise.all(stopPromises);

      expect(system.getAllAgents().length).toBe(0);
    });
  });

  describe('系统事件', () => {
    it('启动应该触发 system:started 事件', async () => {
      const handler = vi.fn();
      system.on('system:started', handler);

      await system.start();

      expect(handler).toHaveBeenCalled();
    });

    it('停止应该触发 system:stopped 事件', async () => {
      await system.start();

      const handler = vi.fn();
      system.on('system:stopped', handler);

      await system.stop();

      expect(handler).toHaveBeenCalled();
    });
  });
});
