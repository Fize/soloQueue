/**
 * ============================================
 * Repository 基类接口 (Base Repository)
 * ============================================
 *
 * 【职责】
 * - 定义所有 Repository 必须实现的接口
 * - 提供数据访问的标准契约
 * - 约束 CRUD 操作的方法签名
 *
 * 【接口方法】
 *
 *   ┌─────────────────────────────────────────────┐
 *   │           Repository<T>                      │
 *   ├─────────────────────────────────────────────┤
 *   │ findById(id: string) → Promise<T | null>    │
 *   │ findAll() → Promise<T[]>                    │
 *   │ create(input) → Promise<T>                  │
 *   │ update(id, input) → Promise<T | null>       │
 *   │ delete(id) → Promise<boolean>               │
 *   └─────────────────────────────────────────────┘
 *
 * 【实现约定】
 *
 *   1. 所有方法都是异步的 (返回 Promise)
 *   2. ID 类型统一为 string (UUID)
 *   3. 找不到时返回 null 而非抛出异常
 *   4. 删除操作返回 boolean 表示是否成功
 *
 * 【日志要求】
 *
 *   每个实现类应记录:
 *   - 操作类型 (CREATE, READ, UPDATE, DELETE)
 *   - 操作对象 (ID, 名称等)
 *   - 操作结果 (成功/失败)
 *
 * ============================================
 */

/**
 * Repository 通用接口
 * @template T - 实体类型
 */
export interface Repository<T> {
  /**
   * 根据 ID 查找单个实体
   */
  findById(id: string): Promise<T | null>;

  /**
   * 查找所有实体
   */
  findAll(): Promise<T[]>;

  /**
   * 创建新实体
   */
  create(input: unknown): Promise<T>;

  /**
   * 更新实体
   * @returns 更新后的实体，如不存在返回 null
   */
  update(id: string, input: unknown): Promise<T | null>;

  /**
   * 删除实体
   * @returns 是否删除成功
   */
  delete(id: string): Promise<boolean>;
}
