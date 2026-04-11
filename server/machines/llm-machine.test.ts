/**
 * ============================================
 * LLM Machine 单元测试
 * ============================================
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createActor } from 'xstate';
import { llmMachine, createLLMActor, DEFAULT_TIMEOUT, DEFAULT_MAX_RETRIES } from './llm-machine.js';

// Mock fetch
const mockFetch = vi.fn();
global.fetch = mockFetch;

describe('LLM Machine', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('初始状态', () => {
    it('初始状态应该是 idle', () => {
      const actor = createActor(llmMachine);
      actor.start();
      expect(actor.getSnapshot().value).toBe('idle');
      actor.stop();
    });

    it('默认配置应该正确', () => {
      const actor = createActor(llmMachine);
      actor.start();
      const ctx = actor.getSnapshot().context;

      expect(ctx.timeout).toBe(DEFAULT_TIMEOUT);
      expect(ctx.maxRetries).toBe(DEFAULT_MAX_RETRIES);
      expect(ctx.model).toBe('deepseek-chat');
      expect(ctx.temperature).toBe(0.7);
      expect(ctx.maxTokens).toBe(2000);
      actor.stop();
    });
  });

  describe('idle 状态', () => {
    it('收到 call 事件应该转换到 loading', () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ content: 'Hello' }),
      });

      const actor = createActor(llmMachine);
      actor.start();

      actor.send({
        type: 'call',
        input: {
          model: 'deepseek-chat',
          messages: [{ role: 'user', content: 'Hi' }],
          temperature: 0.7,
          maxTokens: 2000,
        },
      });

      expect(actor.getSnapshot().value).toBe('loading');
      actor.stop();
    });

    it('idle 状态下收到 reset 应该保持 idle', () => {
      const actor = createActor(llmMachine);
      actor.start();

      actor.send({ type: 'reset' });

      expect(actor.getSnapshot().value).toBe('idle');
      actor.stop();
    });
  });

  describe('loading 状态', () => {
    it('LLM 调用成功应该转换到 success', async () => {
      const mockResponse = {
        content: 'AI response',
        usage: { total_tokens: 100 },
      };
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve(mockResponse),
      });

      const actor = createActor(llmMachine);
      actor.start();

      actor.send({
        type: 'call',
        input: {
          model: 'deepseek-chat',
          messages: [{ role: 'user', content: 'Hi' }],
        },
      });

      // 等待异步调用完成
      await new Promise(resolve => setTimeout(resolve, 100));

      expect(actor.getSnapshot().value).toBe('success');
      actor.stop();
    });

    it('LLM 调用失败应该转换到 failed', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 500,
        json: () => Promise.resolve({ message: 'Server error' }),
      });

      const actor = createActor(llmMachine);
      actor.start();

      actor.send({
        type: 'call',
        input: {
          model: 'deepseek-chat',
          messages: [{ role: 'user', content: 'Hi' }],
        },
      });

      await new Promise(resolve => setTimeout(resolve, 100));

      expect(actor.getSnapshot().value).toBe('failed');
      const ctx = actor.getSnapshot().context;
      expect(ctx.error).toBeTruthy();
      actor.stop();
    });

    it('LLM 调用网络错误应该转换到 failed', async () => {
      mockFetch.mockRejectedValueOnce(new Error('Network error'));

      const actor = createActor(llmMachine);
      actor.start();

      actor.send({
        type: 'call',
        input: {
          model: 'deepseek-chat',
          messages: [{ role: 'user', content: 'Hi' }],
        },
      });

      await new Promise(resolve => setTimeout(resolve, 100));

      expect(actor.getSnapshot().value).toBe('failed');
      actor.stop();
    });
  });

  describe('success 状态', () => {
    it('success 状态下收到 reset 应该返回 idle', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ content: 'test' }),
      });

      const actor = createActor(llmMachine);
      actor.start();

      actor.send({ type: 'call', input: { model: 'test', messages: [], temperature: 0.7, maxTokens: 2000 } });
      await new Promise(resolve => setTimeout(resolve, 100));

      actor.send({ type: 'reset' });

      expect(actor.getSnapshot().value).toBe('idle');
      expect(actor.getSnapshot().context.fullResponse).toBe('');
      actor.stop();
    });
  });

  describe('failed 状态', () => {
    it('failed 状态下可以重试', async () => {
      mockFetch
        .mockRejectedValueOnce(new Error('First error'))
        .mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({ content: 'Success after retry' }),
        });

      const actor = createActor(llmMachine);
      actor.start();

      // 第一次调用失败
      actor.send({ type: 'call', input: { model: 'test', messages: [], temperature: 0.7, maxTokens: 2000 } });
      await new Promise(resolve => setTimeout(resolve, 100));
      expect(actor.getSnapshot().value).toBe('failed');

      // 重试
      actor.send({ type: 'retry' });
      await new Promise(resolve => setTimeout(resolve, 100));
      expect(actor.getSnapshot().value).toBe('success');

      actor.stop();
    });

    it('failed 状态下收到 reset 应该返回 idle', async () => {
      mockFetch.mockRejectedValueOnce(new Error('Error'));

      const actor = createActor(llmMachine);
      actor.start();

      actor.send({ type: 'call', input: { model: 'test', messages: [], temperature: 0.7, maxTokens: 2000 } });
      await new Promise(resolve => setTimeout(resolve, 100));

      actor.send({ type: 'reset' });

      expect(actor.getSnapshot().value).toBe('idle');
      actor.stop();
    });
  });

  describe('工厂函数', () => {
    it('createLLMActor 应该创建正确配置的 actor', () => {
      const actor = createLLMActor({
        model: 'deepseek-coder',
        temperature: 0.9,
        maxTokens: 4000,
        timeout: 60000,
        maxRetries: 5,
      });

      actor.start();
      const ctx = actor.getSnapshot().context;

      expect(ctx.model).toBe('deepseek-coder');
      expect(ctx.temperature).toBe(0.9);
      expect(ctx.maxTokens).toBe(4000);
      expect(ctx.timeout).toBe(60000);
      expect(ctx.maxRetries).toBe(5);

      actor.stop();
    });

    it('createLLMActor 应该使用默认值', () => {
      const actor = createLLMActor();
      actor.start();
      const ctx = actor.getSnapshot().context;

      expect(ctx.model).toBe('deepseek-chat');
      expect(ctx.temperature).toBe(0.7);
      expect(ctx.timeout).toBe(DEFAULT_TIMEOUT);

      actor.stop();
    });
  });
});
