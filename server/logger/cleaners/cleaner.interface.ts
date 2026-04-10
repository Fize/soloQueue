/**
 * 清理器接口
 */

export interface Cleaner {
  clean(): Promise<string[]>;
}
