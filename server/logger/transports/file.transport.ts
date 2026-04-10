/**
 * 文件传输器 - 使用 winston-daily-rotate-file
 */

import { DailyRotateFile } from 'winston-daily-rotate-file';
import winston from 'winston';
import path from 'node:path';
import type { Transport, LogEntry } from './transport.interface.js';
import { LOG_DIR, ROTATE_CONFIG, DATE_FORMAT } from '../config.js';

export class FileTransport implements Transport {
  private transports: Map<string, winston.transport>;

  constructor() {
    this.transports = new Map();
  }

  private getTransportKey(layer: string, category: string): string {
    return `${layer}:${category}`;
  }

  private getTransport(layer: string, category: string): winston.transport {
    const key = this.getTransportKey(layer, category);
    let transport = this.transports.get(key);

    if (!transport) {
      const logPath = path.join(LOG_DIR, layer);
      transport = new DailyRotateFile({
        filename: `${category}-%DATE%.jsonl`,
        dirname: logPath,
        datePattern: DATE_FORMAT,
        maxSize: `${ROTATE_CONFIG.maxSize}b`,
        maxFiles: `${ROTATE_CONFIG.maxDays}d`,
        zippedArchive: false, // 不压缩
        format: winston.format.printf(({ message }) => message + '\n'),
      });

      transport.on('error', (err) => {
        console.error(`[FileTransport] Error on ${key}:`, err);
      });

      this.transports.set(key, transport);
    }

    return transport;
  }

  async log(entry: LogEntry): Promise<void> {
    return new Promise((resolve, reject) => {
      const transport = this.getTransport(entry.layer, entry.category);
      const line = JSON.stringify(entry);

      transport.log({ message: line }, (err) => {
        if (err) reject(err);
        else resolve();
      });
    });
  }

  async flush(): Promise<void> {
    const promises = Array.from(this.transports.values()).map(
      (t) =>
        new Promise<void>((resolve) => {
          if ('flush' in t && typeof t.flush === 'function') {
            t.flush(() => resolve());
          } else {
            resolve();
          }
        })
    );
    await Promise.all(promises);
  }

  async close(): Promise<void> {
    const promises = Array.from(this.transports.values()).map(
      (t) =>
        new Promise<void>((resolve) => {
          t.close();
          resolve();
        })
    );
    await Promise.all(promises);
    this.transports.clear();
  }
}

export const fileTransport = new FileTransport();
