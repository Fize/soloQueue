/**
 * ============================================
 * 数据库 Schema 定义 (Drizzle ORM)
 * ============================================
 *
 * 【职责】
 * - 定义数据库表结构
 * - 提供类型安全的列定义
 * - 导出表对应的 TypeScript 类型
 *
 * 【表关系】
 *
 *   teams ──────────────< agents
 *   (PK: id)            (FK: team_id)
 *     │
 *     │
 *     └─── 1 : N ─────── 每个团队可以有多个 Agent
 *
 *   configs (独立表，无外键关联)
 *   - 全局配置表
 *   - 按 category 分类
 *   - 按 key 唯一索引
 *
 * 【字段命名约定】
 * - 数据库列: snake_case (created_at, team_id)
 * - TypeScript 属性: camelCase (createdAt, teamId)
 * - Drizzle 配置映射转换
 *
 * ============================================
 */

import { sqliteTable, text, integer, real } from 'drizzle-orm/sqlite-core';

// ============== Teams 表 ==============
export const teams = sqliteTable('teams', {
  id: text('id').primaryKey(),
  name: text('name').notNull().unique(),
  description: text('description').notNull().default(''),
  workspaces: text('workspaces').notNull().default('["~/.soloqueue"]'),
  isDefault: integer('is_default', { mode: 'boolean' }).notNull().default(false),
  createdAt: text('created_at').notNull(),
  updatedAt: text('updated_at').notNull(),
});

// ============== Configs 表 ==============
export const configs = sqliteTable('configs', {
  id: integer('id').primaryKey({ autoIncrement: true }),
  key: text('key').notNull().unique(),
  value: text('value').notNull(),
  type: text('type', { enum: ['string', 'number', 'boolean', 'json'] }).notNull().default('string'),
  description: text('description'),
  category: text('category').notNull(),
  editable: integer('editable', { mode: 'boolean' }).notNull().default(true),
  createdAt: text('created_at').notNull(),
  updatedAt: text('updated_at').notNull(),
});

// ============== Agents 表 ==============
export const agents = sqliteTable('agents', {
  id: text('id').primaryKey(),
  teamId: text('team_id').notNull().references(() => teams.id),
  name: text('name').notNull(),
  model: text('model').notNull().default('deepseek-chat'),
  systemPrompt: text('system_prompt').notNull().default(''),
  temperature: real('temperature').notNull().default(0.7),
  maxTokens: integer('max_tokens').notNull().default(2000),
  contextWindow: integer('context_window').notNull().default(64000),
  skills: text('skills').notNull().default('[]'),
  mcp: text('mcp').notNull().default('[]'),
  hooks: text('hooks').notNull().default('[]'),
  createdAt: text('created_at').notNull(),
  updatedAt: text('updated_at').notNull(),
});

// 类型导出
export type TeamRow = typeof teams.$inferSelect;
export type NewTeamRow = typeof teams.$inferInsert;
export type ConfigRow = typeof configs.$inferSelect;
export type NewConfigRow = typeof configs.$inferInsert;
export type AgentRow = typeof agents.$inferSelect;
export type NewAgentRow = typeof agents.$inferInsert;
