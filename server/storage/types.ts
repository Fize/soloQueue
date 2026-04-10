/**
 * 存储层类型定义
 */

export type ID = string;
export type Timestamp = string;

// Team 状态
export enum TeamStatus {
  ACTIVE = 'active',
  INACTIVE = 'inactive',
  ARCHIVED = 'archived',
}

// Team 实体
export interface Team {
  id: ID;
  name: string;
  description: string | null;
  status: TeamStatus;
  config: Record<string, unknown>;
  createdAt: Timestamp;
  updatedAt: Timestamp;
}

// Team 创建参数
export interface CreateTeamInput {
  name: string;
  description?: string;
  config?: Record<string, unknown>;
}

// Team 更新参数
export interface UpdateTeamInput {
  name?: string;
  description?: string;
  status?: TeamStatus;
  config?: Record<string, unknown>;
}

// Team 查询参数
export interface TeamQuery {
  status?: TeamStatus;
}
