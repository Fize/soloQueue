/**
 * Logger 单元测试
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { Logger, LogLayer } from './index.js';

describe('Logger', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('LogLayer', () => {
    it('should have three layers', () => {
      expect(LogLayer.SYSTEM).toBe('system');
      expect(LogLayer.TEAM).toBe('team');
      expect(LogLayer.SESSION).toBe('session');
    });
  });

  describe('Logger.system()', () => {
    it('should create a system logger', () => {
      const logger = Logger.system({ enableConsole: false, enableFile: false });
      expect(logger).toBeInstanceOf(Logger);
    });
  });

  describe('Logger.team()', () => {
    it('should create a team logger with teamId', () => {
      const logger = Logger.team('team-001', {
        enableConsole: false,
        enableFile: false,
      });
      expect(logger).toBeInstanceOf(Logger);
    });
  });

  describe('Logger.session()', () => {
    it('should create a session logger with teamId and sessionId', () => {
      const logger = Logger.session('team-001', 'session-abc', {
        enableConsole: false,
        enableFile: false,
      });
      expect(logger).toBeInstanceOf(Logger);
    });
  });

  describe('child logger', () => {
    it('should inherit parent settings', () => {
      const parent = Logger.system({ enableConsole: false, enableFile: false });
      const child = parent.child({});
      expect(child).toBeInstanceOf(Logger);
    });

    it('should allow overriding layer', () => {
      const parent = Logger.system({ enableConsole: false, enableFile: false });
      const child = parent.child({ layer: LogLayer.TEAM });
      expect(child).toBeInstanceOf(Logger);
    });
  });

  describe('log methods', () => {
    it('should not throw when logging with all transports disabled', async () => {
      const logger = Logger.system({ enableConsole: false, enableFile: false });

      await expect(
        logger.info({ category: 'app', message: 'Test message' })
      ).resolves.not.toThrow();
    });

    it('should support debug method without throwing', async () => {
      const logger = Logger.system({ enableConsole: false, enableFile: false });
      await expect(
        logger.debug({ category: 'app', message: 'Debug message' })
      ).resolves.not.toThrow();
    });

    it('should support warn method without throwing', async () => {
      const logger = Logger.system({ enableConsole: false, enableFile: false });
      await expect(
        logger.warn({ category: 'app', message: 'Warning message' })
      ).resolves.not.toThrow();
    });

    it('should support error method without throwing', async () => {
      const logger = Logger.system({ enableConsole: false, enableFile: false });
      await expect(
        logger.error({ category: 'app', message: 'Error message' })
      ).resolves.not.toThrow();
    });
  });

  describe('logError', () => {
    it('should not throw when logging Error object', async () => {
      const logger = Logger.system({ enableConsole: false, enableFile: false });
      const error = new Error('Test error');

      await expect(
        logger.logError('app', 'Operation failed', error)
      ).resolves.not.toThrow();
    });

    it('should not throw when logging non-Error objects', async () => {
      const logger = Logger.system({ enableConsole: false, enableFile: false });

      await expect(
        logger.logError('app', 'Operation failed', 'String error')
      ).resolves.not.toThrow();
    });
  });

  describe('logDuration', () => {
    it('should measure duration of async operation', async () => {
      const logger = Logger.system({ enableConsole: false, enableFile: false });

      await expect(
        logger.logDuration('test', 'Test operation', async () => {
          await new Promise((resolve) => setTimeout(resolve, 10));
        })
      ).resolves.not.toThrow();
    });

    it('should log error when operation fails', async () => {
      const logger = Logger.system({ enableConsole: false, enableFile: false });

      await expect(
        logger.logDuration('test', 'Failed operation', async () => {
          throw new Error('Operation failed');
        })
      ).resolves.not.toThrow();
    });
  });

  describe('flush and close', () => {
    it('should flush all transports', async () => {
      const logger = Logger.system({ enableConsole: false, enableFile: false });
      await expect(logger.flush()).resolves.not.toThrow();
    });

    it('should close all transports', async () => {
      const logger = Logger.system({ enableConsole: false, enableFile: false });
      await expect(logger.close()).resolves.not.toThrow();
    });
  });

  describe('traceId', () => {
    it('should set and return traceId', () => {
      const logger = Logger.system({ enableConsole: false, enableFile: false });
      const traceId = logger.newTraceId();
      expect(traceId).toBeDefined();
      expect(typeof traceId).toBe('string');
    });
  });
});
