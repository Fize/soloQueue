/**
 * 控制台传输器
 */

import type { Transport, LogEntry } from './transport.interface.js';

const LOG_COLORS: Record<string, string> = {
  debug: '\x1b[36m', // cyan
  info: '\x1b[32m',  // green
  warn: '\x1b[33m',  // yellow
  error: '\x1b[31m', // red
};

const RESET = '\x1b[0m';

export class ConsoleTransport implements Transport {
  private minLevel: string;

  constructor(minLevel: string = 'info') {
    this.minLevel = minLevel;
  }

  private shouldLog(level: string): boolean {
    const levels = ['debug', 'info', 'warn', 'error'];
    return levels.indexOf(level) >= levels.indexOf(this.minLevel);
  }

  async log(entry: LogEntry): Promise<void> {
    if (!this.shouldLog(entry.level)) return;

    const color = LOG_COLORS[entry.level] || '';
    const prefix = `${color}[${entry.level.toUpperCase()}]${RESET}`;
    const timestamp = new Date(entry.timestamp).toLocaleTimeString();
    const meta = entry.context ? ` ${JSON.stringify(entry.context)}` : '';

    console.log(
      `${prefix} ${timestamp} [${entry.category}] ${entry.message}${meta}`
    );

    if (entry.error) {
      console.error(`  Error: ${entry.error.message}`);
      if (entry.error.stack) {
        console.error(entry.error.stack);
      }
    }
  }

  async flush(): Promise<void> {
    // 控制台不需要 flush
  }

  async close(): Promise<void> {
    // 控制台不需要关闭
  }
}

export const consoleTransport = new ConsoleTransport();
