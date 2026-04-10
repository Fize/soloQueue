/**
 * Formatter 单元测试
 */

import { describe, it, expect } from 'vitest';
import { JsonlFormatter } from './jsonl.formatter.js';

describe('JsonlFormatter', () => {
  const formatter = new JsonlFormatter();

  const mockEntry = {
    timestamp: '2026-04-11T00:00:00.000Z',
    level: 'info' as const,
    category: 'app',
    layer: 'system',
    message: 'Test message',
    context: { key: 'value' },
  };

  describe('format()', () => {
    it('should format entry as single line JSON', () => {
      const result = formatter.format(mockEntry);

      expect(result).toBe(JSON.stringify(mockEntry));
    });

    it('should include all fields', () => {
      const entryWithAllFields = {
        ...mockEntry,
        traceId: 'trace-123',
        actorId: 'actor-456',
        teamId: 'team-789',
        sessionId: 'session-abc',
        duration: 100,
        error: {
          name: 'Error',
          message: 'Error message',
          stack: 'Error stack',
        },
      };

      const result = formatter.format(entryWithAllFields);
      const parsed = JSON.parse(result);

      expect(parsed.traceId).toBe('trace-123');
      expect(parsed.actorId).toBe('actor-456');
      expect(parsed.teamId).toBe('team-789');
      expect(parsed.sessionId).toBe('session-abc');
      expect(parsed.duration).toBe(100);
      expect(parsed.error).toEqual(entryWithAllFields.error);
    });

    it('should handle optional fields being undefined', () => {
      const minimalEntry = {
        timestamp: mockEntry.timestamp,
        level: mockEntry.level,
        category: mockEntry.category,
        layer: mockEntry.layer,
        message: 'Minimal message',
      };

      const result = formatter.format(minimalEntry);
      const parsed = JSON.parse(result);

      expect(parsed.context).toBeUndefined();
      expect(parsed.traceId).toBeUndefined();
      expect(parsed.error).toBeUndefined();
    });
  });

  describe('formatBatch()', () => {
    it('should format multiple entries separated by newlines', () => {
      const entries = [
        { ...mockEntry, message: 'Message 1' },
        { ...mockEntry, message: 'Message 2' },
        { ...mockEntry, message: 'Message 3' },
      ];

      const result = formatter.formatBatch(entries);
      const lines = result.trim().split('\n');

      expect(lines).toHaveLength(3);
      expect(JSON.parse(lines[0]).message).toBe('Message 1');
      expect(JSON.parse(lines[1]).message).toBe('Message 2');
      expect(JSON.parse(lines[2]).message).toBe('Message 3');
    });

    it('should end with newline', () => {
      const result = formatter.formatBatch([mockEntry]);

      expect(result.endsWith('\n')).toBe(true);
    });
  });
});
