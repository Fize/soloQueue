/**
 * ============================================
 * Actor 系统核心 - Supervisor (监督者)
 * ============================================
 *
 * 【核心职责】
 * 1. 监督子 Actor 的生命周期
 * 2. 执行监督策略 (OneForOne, OneForAll, AllForOne, Stop)
 * 3. 处理失败和重启
 *
 * 【设计原则】
 * 1. 复用 Logger - 所有操作都记录日志
 * 2. 指数退避 - 避免频繁重启
 * 3. 策略模式 - 支持多种监督策略
 * 4. 配置驱动 - 从配置系统获取默认参数
 *
 * 【配置来源】
 * supervisor.defaults
 *
 */

import type { ActorInstance, SupervisionConfig, SupervisionStrategy } from './types.js';
import type { ActorSystem } from './actor-system.js';
import { Logger } from '../logger/index.js';
import { llmConfigService } from '../llm/index.js';

// ============== 监督记录 ==============

interface WatchedActor {
  instance: ActorInstance;
  config: SupervisionConfig;
  restartCount: number;
  lastRestartTime: number;
  unsubscribe: () => void;
}

// ============== Supervisor ==============

export class Supervisor {
  private watches = new Map<string, WatchedActor>();
  private logger: Logger;
  private defaultSupervisorConfig: ReturnType<typeof llmConfigService.getSupervisorDefaults>;

  constructor(
    private system: ActorSystem,
    logger: Logger
  ) {
    this.logger = logger;
    // 从配置系统获取默认监督配置
    this.defaultSupervisorConfig = llmConfigService.getSupervisorDefaults();
  }

  /**
   * 监督一个 Actor
   */
  watch(instance: ActorInstance): void {
    // 检查是否已在监督中
    if (this.watches.has(instance.id)) {
      this.logger.warn({
        category: 'actor',
        message: 'Actor already being watched',
        context: { agentId: instance.id },
      });
      return;
    }

    // 优先使用定义中的监督配置，否则使用配置系统默认值
    const definitionConfig = instance.metadata.definition?.supervision as SupervisionConfig | undefined;
    const config: SupervisionConfig = definitionConfig || {
      strategy: this.defaultSupervisorConfig.defaultStrategy,
      maxRetries: this.defaultSupervisorConfig.maxRetries,
      retryInterval: this.defaultSupervisorConfig.retryInterval,
      exponentialBackoff: this.defaultSupervisorConfig.exponentialBackoff,
      maxBackoff: this.defaultSupervisorConfig.maxBackoff,
    };

    // 订阅 Actor 状态变化
    const unsubscribe = instance.ref.subscribe({
      next: (snapshot) => {
        this.onStateChange(instance.id, snapshot);
      },
      error: (error) => {
        this.handleFailure(instance, String(error));
      },
    });

    this.watches.set(instance.id, {
      instance,
      config,
      restartCount: 0,
      lastRestartTime: 0,
      unsubscribe,
    });

    this.logger.info({
      category: 'actor',
      message: 'Actor watching started',
      context: {
        agentId: instance.id,
        strategy: config.strategy,
        maxRetries: config.maxRetries,
      },
    });
  }

  /**
   * 取消监督
   */
  unwatch(actorId: string): void {
    const watched = this.watches.get(actorId);
    if (!watched) return;

    watched.unsubscribe();
    this.watches.delete(actorId);

    this.logger.info({
      category: 'actor',
      message: 'Actor unwatched',
      context: { agentId: actorId },
    });
  }

  /**
   * 状态变化处理
   */
  private onStateChange(actorId: string, snapshot: any): void {
    // 检查是否有错误状态
    if (snapshot.status === 'error' || snapshot.value === 'failed') {
      const watched = this.watches.get(actorId);
      if (watched) {
        this.handleFailure(watched.instance, snapshot.error?.message || 'Unknown error');
      }
    }
  }

  /**
   * 处理失败
   */
  private async handleFailure(instance: ActorInstance, error: string): Promise<void> {
    const watched = this.watches.get(instance.id);
    if (!watched) return;

    const { config, restartCount } = watched;

    this.logger.warn({
      category: 'actor',
      message: 'Actor failure detected',
      context: {
        agentId: instance.id,
        error,
        strategy: config.strategy,
        restartCount,
        maxRetries: config.maxRetries,
      },
    });

    // 根据策略处理
    switch (config.strategy) {
      case 'stop':
        await this.handleStop(instance);
        break;

      case 'one_for_one':
        await this.handleOneForOne(watched);
        break;

      case 'one_for_all':
        await this.handleOneForAll(watched);
        break;

      case 'all_for_one':
        await this.handleAllForOne(watched);
        break;
    }
  }

  /**
   * Stop 策略 - 直接停止
   */
  private async handleStop(instance: ActorInstance): Promise<void> {
    this.logger.info({
      category: 'actor',
      message: 'Stop strategy: stopping actor',
      context: { agentId: instance.id },
    });

    await this.system.stopAgent(instance.id);
  }

  /**
   * OneForOne 策略 - 只重启失败的 Actor
   */
  private async handleOneForOne(watched: WatchedActor): Promise<void> {
    const { instance, config, restartCount } = watched;

    // 检查是否超过最大重试次数
    if (restartCount >= config.maxRetries) {
      this.logger.error({
        category: 'actor',
        message: 'Max retries exceeded, stopping actor',
        context: {
          agentId: instance.id,
          restartCount,
          maxRetries: config.maxRetries,
        },
      });

      await this.system.stopAgent(instance.id);
      return;
    }

    // 等待退避时间
    const backoff = this.calculateBackoff(watched);
    if (backoff > 0) {
      await this.delay(backoff);
    }

    // 重启
    watched.restartCount++;
    watched.lastRestartTime = Date.now();

    this.logger.info({
      category: 'actor',
      message: 'Restarting actor (OneForOne)',
      context: {
        agentId: instance.id,
        attempt: watched.restartCount,
        backoff,
      },
    });

    await this.system.restartAgent(instance.id);
  }

  /**
   * OneForAll 策略 - 一个失败，重启所有子 Actor
   */
  private async handleOneForAll(watched: WatchedActor): Promise<void> {
    const { instance, config } = watched;

    if (watched.restartCount >= config.maxRetries) {
      this.logger.error({
        category: 'actor',
        message: 'Max retries exceeded, stopping all children',
        context: { agentId: instance.id },
      });

      // 停止所有子 Actor
      for (const childId of instance.children) {
        await this.system.stopAgent(childId);
      }
      return;
    }

    // 重启所有子 Actor
    for (const childId of instance.children) {
      const backoff = this.calculateBackoff(watched);
      if (backoff > 0) {
        await this.delay(backoff);
      }

      this.logger.info({
        category: 'actor',
        message: 'Restarting child (OneForAll)',
        context: { agentId: childId },
      });

      await this.system.restartAgent(childId);
    }

    watched.restartCount++;
    watched.lastRestartTime = Date.now();
  }

  /**
   * AllForOne 策略 - 一个失败，全部重启（包括自己）
   */
  private async handleAllForOne(watched: WatchedActor): Promise<void> {
    const { instance, config } = watched;

    if (watched.restartCount >= config.maxRetries) {
      this.logger.error({
        category: 'actor',
        message: 'Max retries exceeded, stopping all',
        context: { agentId: instance.id },
      });

      // 停止所有子 Actor
      for (const childId of instance.children) {
        await this.system.stopAgent(childId);
      }

      // 停止自己
      await this.system.stopAgent(instance.id);
      return;
    }

    // 等待退避时间
    const backoff = this.calculateBackoff(watched);
    if (backoff > 0) {
      await this.delay(backoff);
    }

    // 重启所有子 Actor
    for (const childId of instance.children) {
      this.logger.info({
        category: 'actor',
        message: 'Restarting child (AllForOne)',
        context: { agentId: childId },
      });

      await this.system.restartAgent(childId);
    }

    // 重启自己
    this.logger.info({
      category: 'actor',
      message: 'Restarting self (AllForOne)',
      context: { agentId: instance.id },
    });

    await this.system.restartAgent(instance.id);

    watched.restartCount++;
    watched.lastRestartTime = Date.now();

    this.logger.info({
      category: 'actor',
      message: 'All actors restarted (AllForOne)',
      context: {
        agentId: instance.id,
        restartCount: watched.restartCount,
        backoff,
      },
    });
  }

  /**
   * 计算退避时间
   */
  private calculateBackoff(watched: WatchedActor): number {
    const { config, restartCount } = watched;

    if (!config.exponentialBackoff) {
      return config.retryInterval;
    }

    const backoff = config.retryInterval * Math.pow(2, restartCount);
    return Math.min(backoff, config.maxBackoff);
  }

  /**
   * 延迟
   */
  private delay(ms: number): Promise<void> {
    return new Promise(resolve => setTimeout(resolve, ms));
  }

  /**
   * 获取监督状态
   */
  getWatchStatus(): Array<{
    agentId: string;
    strategy: SupervisionStrategy;
    restartCount: number;
    lastRestartTime: number;
  }> {
    return Array.from(this.watches.entries()).map(([id, watched]) => ({
      agentId: id,
      strategy: watched.config.strategy,
      restartCount: watched.restartCount,
      lastRestartTime: watched.lastRestartTime,
    }));
  }
}
