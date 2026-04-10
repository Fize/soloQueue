/**
 * JSONL 格式化器
 * 每行一条 JSON 记录
 */

import type { Formatter, LogEntry } from './formatter.interface.js';

export class JsonlFormatter implements Formatter {
  format(entry: LogEntry): string {
    return JSON.stringify(entry);
  }

  formatBatch(entries: LogEntry[]): string {
    return entries.map((e) => this.format(e)).join('\n') + '\n';
  }
}

export const jsonlFormatter = new JsonlFormatter();
