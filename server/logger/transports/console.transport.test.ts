/**
 * Console Transport 单元测试
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { ConsoleTransport } from './console.transport.js';
import type { LogEntry } from './transport.interface.js';

describe('ConsoleTransport', () => {
  let transport: ConsoleTransport;
  let consoleLogSpy: ReturnType<typeof vi.spyOn>;
  let consoleErrorSpy: ReturnType<typeof vi.spyOn>;

  const createLogEntry = (overrides: Partial<LogEntry> = {}): LogEntry => ({
    id: 'test-id',
    timestamp: '2024-01-01T00:00:00.000Z',
    level: 'info',
    message: 'Test message',
    category: 'test',
    layer: 'system',
    ...overrides,
  });

  beforeEach(() => {
    consoleLogSpy = vi.spyOn(console, 'log').mockImplementation(() => {});
    consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    transport = new ConsoleTransport();
  });

  describe('constructor', () => {
    it('应该使用默认最小级别 info', () => {
      const t = new ConsoleTransport();
      expect(t).toBeDefined();
    });

    it('应该接受自定义最小级别', () => {
      const t = new ConsoleTransport('warn');
      expect(t).toBeDefined();
    });
  });

  describe('log', () => {
    it('应该输出 info 级别日志', async () => {
      const entry = createLogEntry({ level: 'info' });
      
      await transport.log(entry);
      
      expect(consoleLogSpy).toHaveBeenCalled();
      const output = consoleLogSpy.mock.calls[0][0] as string;
      expect(output).toContain('[INFO]');
      expect(output).toContain('Test message');
      expect(output).toContain('test');
    });

    it('默认 minLevel=info，应该跳过 debug 级别', async () => {
      const entry = createLogEntry({ level: 'debug' });
      
      await transport.log(entry);
      
      // debug < info，所以被跳过
      expect(consoleLogSpy).not.toHaveBeenCalled();
    });

    it('minLevel=debug，应该输出 debug 级别', async () => {
      const debugTransport = new ConsoleTransport('debug');
      const entry = createLogEntry({ level: 'debug' });
      
      await debugTransport.log(entry);
      
      expect(consoleLogSpy).toHaveBeenCalled();
      const output = consoleLogSpy.mock.calls[0][0] as string;
      expect(output).toContain('[DEBUG]');
    });

    it('应该输出 warn 级别日志', async () => {
      const entry = createLogEntry({ level: 'warn' });
      
      await transport.log(entry);
      
      expect(consoleLogSpy).toHaveBeenCalled();
      const output = consoleLogSpy.mock.calls[0][0] as string;
      expect(output).toContain('[WARN]');
    });

    it('应该输出 error 级别日志', async () => {
      const entry = createLogEntry({ level: 'error' });
      
      await transport.log(entry);
      
      expect(consoleLogSpy).toHaveBeenCalled();
      const output = consoleLogSpy.mock.calls[0][0] as string;
      expect(output).toContain('[ERROR]');
    });

    it('应该包含上下文信息', async () => {
      const entry = createLogEntry({
        context: { userId: '123', action: 'login' },
      });
      
      await transport.log(entry);
      
      expect(consoleLogSpy).toHaveBeenCalled();
      const output = consoleLogSpy.mock.calls[0][0] as string;
      expect(output).toContain('userId');
      expect(output).toContain('123');
    });

    it('应该输出错误详情', async () => {
      const entry = createLogEntry({
        level: 'error',
        error: new Error('Test error'),
      });
      
      await transport.log(entry);
      
      expect(consoleErrorSpy).toHaveBeenCalled();
      const errorOutput = consoleErrorSpy.mock.calls[0][0] as string;
      expect(errorOutput).toContain('Test error');
    });

    it('应该输出错误堆栈', async () => {
      const error = new Error('Test error');
      error.stack = 'Error: Test error\n    at test (test.ts:1)';
      
      const entry = createLogEntry({
        level: 'error',
        error,
      });
      
      await transport.log(entry);
      
      // 应该调用了两次 console.error: 一次是消息，一次是堆栈
      expect(consoleErrorSpy).toHaveBeenCalledTimes(2);
    });

    it('minLevel=error，应该跳过低于 error 的日志', () => {
      const errorTransport = new ConsoleTransport('error');
      const entry = createLogEntry({ level: 'warn' });
      
      errorTransport.log(entry);
      
      expect(consoleLogSpy).not.toHaveBeenCalled();
    });
  });

  describe('flush', () => {
    it('不应该输出任何内容', async () => {
      await transport.flush();
      
      expect(consoleLogSpy).not.toHaveBeenCalled();
      expect(consoleErrorSpy).not.toHaveBeenCalled();
    });
  });

  describe('close', () => {
    it('不应该输出任何内容', async () => {
      await transport.close();
      
      expect(consoleLogSpy).not.toHaveBeenCalled();
      expect(consoleErrorSpy).not.toHaveBeenCalled();
    });
  });
});
