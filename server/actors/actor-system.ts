/**
 * ============================================
 * Actor 系统核心 - ActorSystem
 * ============================================
 *
 * 【核心职责】
 * 1. 管理所有 Actor 的注册和生命周期
 * 2. 提供消息传递机制
 * 3. 协调 Supervisor、Router、Persister
 * 4. 扩展点：工厂注册、路由策略
 *
 * 【设计原则】
 * 1. 复用优于重写 - 直接使用已有的工厂函数和服务
 * 2. 日志优先 - 所有关键操作都记录日志
 * 3. 优雅停止 - 支持优雅关闭所有 Actor
 *
 */

import { EventEmitter } from 'events';
import { createAgentActor } from '../machines/agent-machine.js';
import { AgentService } from '../storage/agent.service.js';
import { Logger } from '../logger/index.js';
import { llmConfigService } from '../llm/index.js';
import type {
  ActorInstance,
  AgentDefinition,
  AgentKind,
  AgentRole,
  ActorMessage,
  TaskMessage,
  ResultMessage,
  SupervisionConfig,
  SystemStatus,
} from './types.js';
import type { AgentFactory, RoutingStrategy, AgentCreateParams } from './extensions.js';
import { Supervisor } from './supervisor.js';
import { Router, RoundRobinStrategy } from './router.js';

// ============== ActorSystem ==============

export class ActorSystem extends EventEmitter {
  // ===== 核心存储 =====
  private registry: Map<string, ActorInstance> = new Map();
  
  // ===== 系统组件 =====
  private supervisor: Supervisor;
  private router: Router;
  
  // ===== 依赖服务 (复用已有) =====
  private agentService: AgentService;
  private logger: Logger;
  
  // ===== 扩展点 =====
  private factories: Map<AgentKind, AgentFactory> = new Map();
  private routingStrategy: RoutingStrategy;
  
  // ===== 状态 =====
  private status: SystemStatus = 'initializing';
  
  // ===== 统计 =====
  private stats = {
    createdAgents: 0,
    stoppedAgents: 0,
    failedAgents: 0,
    totalMessages: 0,
  };

  constructor() {
    super();
    
    // 初始化日志 - 复用已有的 Logger
    this.logger = Logger.system({ 
      enableConsole: true, 
      enableFile: true, 
      minLevel: 'debug' 
    });
    
    // 初始化依赖服务 - 复用已有的 AgentService
    this.agentService = new AgentService();
    
    // 初始化系统组件
    this.supervisor = new Supervisor(this, this.logger);
    this.router = new Router(this);
    
    // 默认路由策略
    this.routingStrategy = new RoundRobinStrategy();
    
    this.logger.info({
      category: 'actor',
      message: 'ActorSystem instance created',
    });
  }

  // ============== 生命周期 ==============

  /**
   * 启动 Actor 系统
   */
  async start(): Promise<void> {
    if (this.status !== 'initializing' && this.status !== 'stopped') {
      this.logger.warn({
        category: 'actor',
        message: 'ActorSystem already started',
        context: { currentStatus: this.status },
      });
      return;
    }

    this.logger.info({
      category: 'actor',
      message: 'ActorSystem starting',
      context: { status: this.status },
    });

    this.status = 'initializing';

    try {
      // 1. 恢复用户 Agent
      await this.restoreUserAgents();

      // 2. 启动完成
      this.status = 'running';
      
      this.logger.info({
        category: 'actor',
        message: 'ActorSystem started successfully',
        context: { 
          registrySize: this.registry.size,
          stats: this.stats,
        },
      });

      this.emit('system:started');
    } catch (error) {
      this.status = 'stopped';
      this.logger.error({
        category: 'actor',
        message: 'ActorSystem start failed',
        context: {
          error: error instanceof Error ? error.message : String(error),
        },
      });
      throw error;
    }
  }

  /**
   * 停止 Actor 系统
   */
  async stop(): Promise<void> {
    if (this.status === 'stopped') {
      return;
    }

    this.logger.info({
      category: 'actor',
      message: 'ActorSystem stopping',
      context: { registrySize: this.registry.size },
    });

    this.status = 'stopping';

    // 优雅停止所有 Agent
    const stopPromises: Promise<void>[] = [];
    for (const [id] of this.registry) {
      stopPromises.push(this.stopAgent(id));
    }

    await Promise.allSettled(stopPromises);

    this.status = 'stopped';

    this.logger.info({
      category: 'actor',
      message: 'ActorSystem stopped',
      context: { stats: this.stats },
    });

    this.emit('system:stopped');
  }

  // ============== Agent 管理 ==============

  /**
   * 创建用户 Agent
   */
  async createAgent(params: AgentCreateParams): Promise<ActorInstance> {
    // 1. 验证状态
    if (this.status !== 'running') {
      throw new Error('ActorSystem is not running');
    }

    // 2. 获取工厂
    const factory = this.factories.get(params.kind);
    if (!factory) {
      throw new Error(`No factory for agent kind: ${params.kind}`);
    }

    // 3. 验证配置
    if (factory.validate) {
      const isValid = factory.validate(params as Partial<AgentDefinition>);
      if (!isValid) {
        throw new Error(`Invalid configuration for agent kind: ${params.kind}`);
      }
    }

    // 4. 生成 ID
    const id = params.id || crypto.randomUUID();

    // 5. 检查是否已存在
    if (this.registry.has(id)) {
      throw new Error(`Agent already exists: ${id}`);
    }

    // 6. 获取默认监督配置
    const supervisorDefaults = llmConfigService.getSupervisorDefaults();

    // 7. 构建定义
    const definition: AgentDefinition = {
      id,
      name: params.name,
      teamId: params.teamId,
      role: 'user',
      kind: params.kind,
      modelId: params.modelId || 'deepseek-chat',
      providerId: params.providerId || 'deepseek',
      systemPrompt: params.systemPrompt || '',
      capabilities: params.capabilities || [],
      tools: params.tools,
      supervision: {
        strategy: supervisorDefaults.defaultStrategy,
        maxRetries: supervisorDefaults.maxRetries,
        retryInterval: supervisorDefaults.retryInterval,
        exponentialBackoff: supervisorDefaults.exponentialBackoff,
        maxBackoff: supervisorDefaults.maxBackoff,
        ...params.supervision,
      },
      enabled: true,
      createdAt: Date.now(),
      updatedAt: Date.now(),
    };

    // 8. 持久化 - 复用 AgentService
    try {
      await this.agentService.create(definition);
    } catch (error) {
      this.logger.error({
        category: 'actor',
        message: 'Failed to persist agent definition',
        context: {
          agentId: id,
          error: error instanceof Error ? error.message : String(error),
        },
      });
    }

    // 8. 创建实例
    const instance = this.createAgentInstance(definition);

    // 9. 注册
    this.registry.set(id, instance);
    this.supervisor.watch(instance);

    // 10. 统计
    this.stats.createdAgents++;

    this.logger.info({
      category: 'actor',
      message: 'Agent created',
      context: {
        agentId: id,
        name: params.name,
        kind: params.kind,
        teamId: params.teamId,
      },
    });

    this.emit('agent:started', { agentId: id, kind: params.kind });

    return instance;
  }

  /**
   * 使用工厂创建 Agent 实例
   */
  private createAgentInstance(definition: AgentDefinition): ActorInstance {
    const factory = this.factories.get(definition.kind);
    if (!factory) {
      throw new Error(`No factory for kind: ${definition.kind}`);
    }

    const instance = factory.create(definition, this);

    // 订阅状态变化，用于日志和监督
    instance.ref.subscribe((snapshot) => {
      this.logger.debug({
        category: 'actor',
        message: 'Agent state changed',
        context: {
          agentId: instance.id,
          state: snapshot.value,
        },
      });

      this.emit('agent:stateChange', {
        agentId: instance.id,
        state: snapshot.value,
        context: snapshot.context,
      });
    });

    return instance;
  }

  /**
   * 停止 Agent
   */
  async stopAgent(id: string): Promise<void> {
    const instance = this.registry.get(id);
    if (!instance) {
      this.logger.warn({
        category: 'actor',
        message: 'Agent not found for stop',
        context: { agentId: id },
      });
      return;
    }

    // 停止子 Agent
    for (const childId of instance.children) {
      await this.stopAgent(childId);
    }

    // 停止自身
    instance.ref.stop();
    this.registry.delete(id);

    // 更新统计
    this.stats.stoppedAgents++;

    // 如果是用户 Agent，更新持久化
    if (instance.role === 'user') {
      try {
        await this.agentService.updateStatus(id, 'stopped');
      } catch (error) {
        this.logger.error({
          category: 'actor',
          message: 'Failed to update agent status',
          context: {
            agentId: id,
            error: error instanceof Error ? error.message : String(error),
          },
        });
      }
    }

    this.logger.info({
      category: 'actor',
      message: 'Agent stopped',
      context: { agentId: id },
    });

    this.emit('agent:stopped', { agentId: id });
  }

  /**
   * 删除用户 Agent
   */
  async deleteAgent(id: string): Promise<void> {
    const instance = this.registry.get(id);
    if (!instance) {
      throw new Error(`Agent not found: ${id}`);
    }

    // 检查是否是系统 Agent
    if (instance.role === 'system') {
      throw new Error('Cannot delete system agent');
    }

    // 停止并删除
    await this.stopAgent(id);

    // 从数据库删除
    try {
      await this.agentService.delete(id);
    } catch (error) {
      this.logger.error({
        category: 'actor',
        message: 'Failed to delete agent from database',
        context: {
          agentId: id,
          error: error instanceof Error ? error.message : String(error),
        },
      });
    }

    this.logger.info({
      category: 'actor',
      message: 'Agent deleted',
      context: { agentId: id },
    });
  }

  // ============== 消息传递 ==============

  /**
   * 发送消息 - 分发到目标 Agent
   */
  dispatch(message: ActorMessage): void {
    this.stats.totalMessages++;

    if (message.type === 'task') {
      // 路由消息
      const target = this.router.route(message as TaskMessage);
      if (target) {
        target.ref.send(message);
        
        this.logger.debug({
          category: 'actor',
          message: 'Message dispatched via router',
          context: {
            taskId: (message as TaskMessage).taskId,
            targetId: target.id,
            contentLength: (message as TaskMessage).content.length,
          },
        });
      } else {
        this.logger.warn({
          category: 'actor',
          message: 'No target found for task message',
          context: {
            content: (message as TaskMessage).content.substring(0, 100),
          },
        });
      }
    } else {
      // 直接发送
      const target = this.registry.get(message.to || '');
      if (target) {
        target.ref.send(message);
      } else {
        this.logger.warn({
          category: 'actor',
          message: 'Message target not found',
          context: {
            type: message.type,
            to: message.to,
          },
        });
      }
    }
  }

  /**
   * 请求-响应模式
   */
  async ask(message: ActorMessage, timeout = 30000): Promise<ResultMessage> {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        cleanup();
        reject(new Error(`Request timeout: ${timeout}ms`));
      }, timeout);

      const replyId = `reply-${crypto.randomUUID()}`;

      const handler = (result: ResultMessage) => {
        if (result.to === replyId) {
          cleanup();
          resolve(result);
        }
      };

      const cleanup = () => {
        clearTimeout(timer);
        this.off('result', handler);
      };

      this.on('result', handler);

      // 发送消息，标记回复目标
      this.dispatch({
        ...message,
        replyTo: replyId,
      });
    });
  }

  /**
   * 广播消息给所有 Agent
   */
  broadcast(message: ActorMessage): void {
    let successCount = 0;
    let failCount = 0;

    for (const [id, instance] of this.registry) {
      try {
        instance.ref.send(message);
        successCount++;
      } catch (error) {
        failCount++;
        this.logger.error({
          category: 'actor',
          message: 'Broadcast failed for agent',
          context: {
            agentId: id,
            error: error instanceof Error ? error.message : String(error),
          },
        });
      }
    }

    this.logger.debug({
      category: 'actor',
      message: 'Broadcast completed',
      context: { successCount, failCount, totalCount: this.registry.size },
    });
  }

  // ============== 扩展点 ==============

  /**
   * 注册 Agent 工厂
   */
  registerFactory(factory: AgentFactory): void {
    if (this.factories.has(factory.kind)) {
      throw new Error(`Factory already registered for kind: ${factory.kind}`);
    }

    this.factories.set(factory.kind, factory);

    this.logger.info({
      category: 'actor',
      message: 'Agent factory registered',
      context: { kind: factory.kind },
    });
  }

  /**
   * 设置路由策略
   */
  setRoutingStrategy(strategy: RoutingStrategy): void {
    this.routingStrategy = strategy;

    this.logger.info({
      category: 'actor',
      message: 'Routing strategy changed',
      context: { strategy: strategy.name },
    });
  }

  // ============== 查询 ==============

  /**
   * 获取 Agent
   */
  getAgent(id: string): ActorInstance | undefined {
    return this.registry.get(id);
  }

  /**
   * 获取所有 Agent
   */
  getAllAgents(): ActorInstance[] {
    return Array.from(this.registry.values());
  }

  /**
   * 按类型获取 Agent
   */
  getAgentsByKind(kind: AgentKind): ActorInstance[] {
    return this.getAllAgents().filter(a => a.kind === kind);
  }

  /**
   * 按角色获取 Agent
   */
  getAgentsByRole(role: AgentRole): ActorInstance[] {
    return this.getAllAgents().filter(a => a.role === role);
  }

  /**
   * 按 Team 获取 Agent
   */
  getAgentsByTeam(teamId: string): ActorInstance[] {
    return this.getAllAgents().filter(a => 
      a.metadata.definition?.teamId === teamId
    );
  }

  /**
   * 获取系统状态
   */
  getStatus(): { status: SystemStatus; stats: typeof this.stats } {
    return {
      status: this.status,
      stats: { ...this.stats },
    };
  }

  // ============== 内部方法 (供 Supervisor 使用) ==============

  /**
   * 处理 Agent 失败
   */
  handleAgentFailure(agentId: string, error: string): void {
    this.stats.failedAgents++;

    this.logger.error({
      category: 'actor',
      message: 'Agent failure',
      context: { agentId, error },
    });

    this.emit('agent:failed', { agentId, error });
  }

  /**
   * 重启 Agent
   */
  async restartAgent(agentId: string): Promise<ActorInstance | null> {
    const oldInstance = this.registry.get(agentId);
    if (!oldInstance) {
      return null;
    }

    const definition = oldInstance.metadata.definition as AgentDefinition;

    // 停止旧实例
    await this.stopAgent(agentId);

    // 重新创建并注册
    const instance = this.createAgentInstance(definition);
    this.registry.set(agentId, instance);
    this.supervisor.watch(instance);

    this.logger.info({
      category: 'actor',
      message: 'Agent restarted',
      context: { agentId },
    });

    return instance;
  }

  // ============== 恢复逻辑 ==============

  /**
   * 从数据库恢复用户 Agent
   */
  private async restoreUserAgents(): Promise<void> {
    try {
      const agents = await this.agentService.findByRole('user');

      this.logger.info({
        category: 'actor',
        message: 'Restoring user agents',
        context: { count: agents.length },
      });

      for (const def of agents) {
        if (!def.enabled) continue;

        try {
          const instance = this.createAgentInstance(def);
          instance.metadata.restored = true;
          this.registry.set(def.id, instance);
          this.supervisor.watch(instance);

          this.logger.info({
            category: 'actor',
            message: 'Agent restored',
            context: { agentId: def.id, kind: def.kind },
          });
        } catch (error) {
          this.logger.error({
            category: 'actor',
            message: 'Failed to restore agent',
            context: {
              agentId: def.id,
              error: error instanceof Error ? error.message : String(error),
            },
          });
        }
      }
    } catch (error) {
      this.logger.error({
        category: 'actor',
        message: 'Failed to restore user agents',
        context: {
          error: error instanceof Error ? error.message : String(error),
        },
      });
    }
  }
}
