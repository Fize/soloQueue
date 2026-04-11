/**
 * ============================================
 * Agent Machine 单元测试
 * ============================================
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createActor } from 'xstate';
import {
  agentMachine,
  createAgentActor,
  DEFAULT_MODEL,
  DEFAULT_TEMPERATURE,
  DEFAULT_MAX_TOKENS,
  MAX_ERROR_COUNT,
} from './agent-machine.js';
import { DEFAULT_AGENT_DEFAULTS } from '../llm/defaults.js';

// Mock llmConfigService
vi.mock('../llm/index.js', () => ({
  llmConfigService: {
    getAgentDefaults: vi.fn(() => DEFAULT_AGENT_DEFAULTS),
  },
}));

// Mock fetch
const mockFetch = vi.fn();
global.fetch = mockFetch;

describe('Agent Machine', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('初始状态', () => {
    it('初始状态应该是 idle', () => {
      const actor = createActor(agentMachine, {
        input: { agentId: 'agent-1', teamId: 'team-1' },
      });
      actor.start();
      expect(actor.getSnapshot().value).toBe('idle');
      actor.stop();
    });

    it('默认配置应该正确', () => {
      const actor = createActor(agentMachine, {
        input: { agentId: 'agent-1', teamId: 'team-1' },
      });
      actor.start();
      const ctx = actor.getSnapshot().context;

      expect(ctx.agentId).toBe('agent-1');
      expect(ctx.teamId).toBe('team-1');
      expect(ctx.model).toBe(DEFAULT_MODEL);
      expect(ctx.temperature).toBe(DEFAULT_TEMPERATURE);
      expect(ctx.maxTokens).toBe(DEFAULT_MAX_TOKENS);
      expect(ctx.errorCount).toBe(0);
      actor.stop();
    });
  });

  describe('idle 状态', () => {
    it('收到 task 事件应该转换到 processing', () => {
      const actor = createActor(agentMachine, {
        input: { agentId: 'agent-1', teamId: 'team-1' },
      });
      actor.start();

      actor.send({
        type: 'task',
        content: 'Hello, how are you?',
        from: 'user',
        taskId: 'task-1',
      });

      expect(actor.getSnapshot().value).toBe('processing');
      actor.stop();
    });

    it('收到 task 事件应该添加用户消息', () => {
      const actor = createActor(agentMachine, {
        input: { agentId: 'agent-1', teamId: 'team-1' },
      });
      actor.start();

      actor.send({
        type: 'task',
        content: 'Hello',
        from: 'user',
        taskId: 'task-1',
      });

      const ctx = actor.getSnapshot().context;
      expect(ctx.messages.length).toBe(1);
      expect(ctx.messages[0].role).toBe('user');
      expect(ctx.messages[0].content).toBe('Hello');
      actor.stop();
    });

    it('收到 task 事件应该记录当前任务', () => {
      const actor = createActor(agentMachine, {
        input: { agentId: 'agent-1', teamId: 'team-1' },
      });
      actor.start();

      actor.send({
        type: 'task',
        content: 'Task content',
        from: 'user',
        taskId: 'task-123',
      });

      const ctx = actor.getSnapshot().context;
      expect(ctx.currentTask).not.toBeNull();
      expect(ctx.currentTask?.id).toBe('task-123');
      actor.stop();
    });

    it('收到 task 事件应该清除之前的错误消息', () => {
      const actor = createActor(agentMachine, {
        input: { agentId: 'agent-1', teamId: 'team-1' },
      });
      actor.start();

      // 先模拟一个错误状态
      actor.send({ type: 'task', content: 'first', from: 'user', taskId: 't1' });
      actor.send({ type: 'error', error: 'Previous error' });

      // 再次收到任务
      actor.send({ type: 'task', content: 'second', from: 'user', taskId: 't2' });

      const ctx = actor.getSnapshot().context;
      // lastError 被清除，但 errorCount 保留（错误计数累积）
      expect(ctx.lastError).toBeNull();
      expect(ctx.errorCount).toBe(1);
      actor.stop();
    });
  });

  describe('processing 状态', () => {
    it('收到 respond 事件应该转换到 responding', () => {
      const actor = createActor(agentMachine, {
        input: { agentId: 'agent-1', teamId: 'team-1' },
      });
      actor.start();

      actor.send({ type: 'task', content: 'Hello', from: 'user', taskId: 't1' });
      actor.send({ type: 'respond', content: 'I am doing well!' });

      expect(actor.getSnapshot().value).toBe('responding');
      actor.stop();
    });

    it('收到 respond 事件应该添加助手消息', () => {
      const actor = createActor(agentMachine, {
        input: { agentId: 'agent-1', teamId: 'team-1' },
      });
      actor.start();

      actor.send({ type: 'task', content: 'Hello', from: 'user', taskId: 't1' });
      actor.send({ type: 'respond', content: 'Hi there!' });

      const ctx = actor.getSnapshot().context;
      expect(ctx.messages.length).toBe(2); // user + assistant
      expect(ctx.messages[1].role).toBe('assistant');
      actor.stop();
    });

    it('收到 error 事件应该返回 idle 并记录错误', () => {
      const actor = createActor(agentMachine, {
        input: { agentId: 'agent-1', teamId: 'team-1' },
      });
      actor.start();

      actor.send({ type: 'task', content: 'Hello', from: 'user', taskId: 't1' });
      actor.send({ type: 'error', error: 'Processing failed' });

      expect(actor.getSnapshot().value).toBe('idle');
      const ctx = actor.getSnapshot().context;
      expect(ctx.errorCount).toBe(1);
      expect(ctx.lastError).toBe('Processing failed');
      actor.stop();
    });
  });

  describe('responding 状态', () => {
    it('收到 respond.complete 应该返回 idle', () => {
      const actor = createActor(agentMachine, {
        input: { agentId: 'agent-1', teamId: 'team-1' },
      });
      actor.start();

      actor.send({ type: 'task', content: 'Hello', from: 'user', taskId: 't1' });
      actor.send({ type: 'respond', content: 'Hi!' });
      actor.send({ type: 'respond.complete' });

      expect(actor.getSnapshot().value).toBe('idle');
      actor.stop();
    });

    it('任务完成后应该清除当前任务', () => {
      const actor = createActor(agentMachine, {
        input: { agentId: 'agent-1', teamId: 'team-1' },
      });
      actor.start();

      actor.send({ type: 'task', content: 'Hello', from: 'user', taskId: 't1' });
      actor.send({ type: 'respond', content: 'Hi!' });
      actor.send({ type: 'respond.complete' });

      const ctx = actor.getSnapshot().context;
      expect(ctx.currentTask).toBeNull();
      actor.stop();
    });
  });

  describe('错误处理', () => {
    it('错误应该累积计数', () => {
      const actor = createActor(agentMachine, {
        input: { agentId: 'agent-1', teamId: 'team-1' },
      });
      actor.start();

      // 第一次错误: idle -> processing -> idle (errorCount = 1)
      actor.send({ type: 'task', content: '1', from: 'u', taskId: 't1' });
      actor.send({ type: 'error', error: 'Error 1' });

      // 检查第一次错误
      expect(actor.getSnapshot().context.errorCount).toBe(1);

      // 第二次错误: idle -> processing -> idle (errorCount = 2)
      actor.send({ type: 'task', content: '2', from: 'u', taskId: 't2' });
      actor.send({ type: 'error', error: 'Error 2' });

      expect(actor.getSnapshot().context.errorCount).toBe(2);
      actor.stop();
    });

    it('MAX_ERROR_COUNT 应该为 3', () => {
      expect(MAX_ERROR_COUNT).toBe(3);
    });
  });

  describe('工厂函数', () => {
    it('createAgentActor 应该创建正确配置的 actor', () => {
      const actor = createAgentActor({
        agentId: 'custom-agent',
        teamId: 'custom-team',
        model: 'deepseek-coder',
        temperature: 0.5,
        maxTokens: 4000,
        systemPrompt: 'You are a helpful assistant.',
      });

      actor.start();
      const ctx = actor.getSnapshot().context;

      expect(ctx.agentId).toBe('custom-agent');
      expect(ctx.teamId).toBe('custom-team');
      expect(ctx.model).toBe('deepseek-coder');
      expect(ctx.temperature).toBe(0.5);
      expect(ctx.maxTokens).toBe(4000);
      expect(ctx.systemPrompt).toBe('You are a helpful assistant.');

      actor.stop();
    });

    it('createAgentActor 应该使用配置服务提供的默认值', () => {
      const actor = createAgentActor({
        agentId: 'agent-1',
        teamId: 'team-1',
      });

      actor.start();
      const ctx = actor.getSnapshot().context;

      // 从配置服务获取的 chat 默认值
      const defaultChatConfig = DEFAULT_AGENT_DEFAULTS.roleDefaults.chat;
      expect(ctx.model).toBe(defaultChatConfig.modelId);
      expect(ctx.temperature).toBe(defaultChatConfig.temperature);
      expect(ctx.maxTokens).toBe(defaultChatConfig.maxTokens);

      actor.stop();
    });
  });

  describe('LLM 调用', () => {
    it('收到 llm 事件应该转换到 waitingLLM', () => {
      const actor = createActor(agentMachine, {
        input: { agentId: 'agent-1', teamId: 'team-1' },
      });
      actor.start();

      actor.send({ type: 'task', content: 'Hello', from: 'user', taskId: 't1' });
      actor.send({ type: 'llm', prompt: 'Generate a response' });

      expect(actor.getSnapshot().value).toBe('waitingLLM');
      actor.stop();
    });

    it('waitingLLM 状态应该有 currentLLMCall', () => {
      const actor = createActor(agentMachine, {
        input: { agentId: 'agent-1', teamId: 'team-1' },
      });
      actor.start();

      actor.send({ type: 'task', content: 'Hello', from: 'user', taskId: 't1' });
      actor.send({ type: 'llm', prompt: 'Generate a response' });

      const ctx = actor.getSnapshot().context;
      expect(ctx.currentLLMCall).not.toBeNull();
      expect(ctx.currentLLMCall?.status).toBe('pending');
      actor.stop();
    });

    it('应该处理 llm.chunk 事件', () => {
      const actor = createActor(agentMachine, {
        input: { agentId: 'agent-1', teamId: 'team-1' },
      });
      actor.start();

      actor.send({ type: 'task', content: 'Hello', from: 'user', taskId: 't1' });
      actor.send({ type: 'llm', prompt: 'Generate' });

      // 模拟流式响应
      actor.send({ type: 'llm.chunk', content: 'Hello' });
      actor.send({ type: 'llm.chunk', content: ' World' });

      const ctx = actor.getSnapshot().context;
      expect(ctx.currentLLMCall?.content).toBe('Hello World');
      expect(ctx.currentLLMCall?.status).toBe('streaming');
      actor.stop();
    });
  });

  describe('委托功能', () => {
    it('收到 delegate 事件应该转换到 delegating', () => {
      const actor = createActor(agentMachine, {
        input: { agentId: 'agent-1', teamId: 'team-1' },
      });
      actor.start();

      actor.send({ type: 'task', content: 'Delegate this', from: 'user', taskId: 't1' });
      actor.send({ type: 'delegate', taskId: 'subtask-1', instruction: 'Do something' });

      expect(actor.getSnapshot().value).toBe('delegating');
      actor.stop();
    });
  });

  describe('初始消息', () => {
    it('应该支持初始消息', () => {
      const initialMessages = [
        { id: '1', role: 'user' as const, content: 'Hello', timestamp: Date.now() },
      ];
      const actor = createActor(agentMachine, {
        input: { agentId: 'agent-1', teamId: 'team-1', initialMessages },
      });
      actor.start();

      const ctx = actor.getSnapshot().context;
      expect(ctx.messages.length).toBe(1);
      expect(ctx.messages[0].content).toBe('Hello');

      actor.stop();
    });
  });
});
