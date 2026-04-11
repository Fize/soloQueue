/**
 * ============================================
 * 存储层类型定义 (Domain Models)
 * ============================================
 *
 * 【职责】
 * - 定义领域模型的 TypeScript 接口
 * - 定义 CRUD 操作的输入类型
 * - 提供跨模块共享的类型定义
 *
 * 【类型层级】
 *
 *   Raw Types (Schema 层)
 *   └── TeamRow, ConfigRow, AgentRow (Drizzle 自动生成)
 *           │
 *           ▼ (转换)
 *   Domain Types (本文件)
 *   └── Team, Config, Agent (业务模型)
 *           │
 *           ▼ (用于 API)
 *   Input Types
 *   └── CreateXxxInput, UpdateXxxInput (API 输入)
 *
 * 【JSON 序列化字段】
 *
 *   以下字段存储为 JSON 字符串:
 *   - Team.workspaces: string[] → '["dir1", "dir2"]'
 *   - Agent.skills: string[] → '["skill1", "skill2"]'
 *   - Agent.mcp: string[] → '["mcp1", "mcp2"]'
 *   - Agent.hooks: string[] → '["hook1", "hook2"]'
 *
 *   Repository 层负责 JSON 的序列化/反序列化
 *
 * ============================================
 */

export type ID = string;
export type Timestamp = string;

// ============== Team ==============
export enum TeamStatus {
  ACTIVE = 'active',
  INACTIVE = 'inactive',
  ARCHIVED = 'archived',
}

export interface Team {
  id: string;
  name: string;
  description: string | null;
  workspaces: string[];
  isDefault: boolean;
  createdAt: Timestamp;
  updatedAt: Timestamp;
}

export interface CreateTeamInput {
  name: string;
  description?: string;
  workspaces?: string[];
}

export interface UpdateTeamInput {
  name?: string;
  description?: string;
  workspaces?: string[];
}

// ============== Config ==============
export type ConfigType = 'string' | 'number' | 'boolean' | 'json';

export interface Config {
  id: number;
  key: string;
  value: string;
  type: ConfigType;
  description: string | null;
  category: string;
  editable: boolean;
  createdAt: Timestamp;
  updatedAt: Timestamp;
}

export interface CreateConfigInput {
  key: string;
  value: string;
  type: ConfigType;
  description?: string;
  category: string;
  editable?: boolean;
}

// ============== Agent ==============
export interface Agent {
  id: string;
  teamId: string;
  name: string;
  model: string;
  systemPrompt: string;
  temperature: number;
  maxTokens: number;
  contextWindow: number;
  skills: string[];
  mcp: string[];
  hooks: string[];
  createdAt: Timestamp;
  updatedAt: Timestamp;
}

export interface CreateAgentInput {
  teamId?: string;
  name: string;
  model?: string;
  systemPrompt?: string;
  temperature?: number;
  maxTokens?: number;
  contextWindow?: number;
  skills?: string[];
  mcp?: string[];
  hooks?: string[];
}

export interface UpdateAgentInput {
  name?: string;
  model?: string;
  systemPrompt?: string;
  temperature?: number;
  maxTokens?: number;
  contextWindow?: number;
  skills?: string[];
  mcp?: string[];
  hooks?: string[];
}
