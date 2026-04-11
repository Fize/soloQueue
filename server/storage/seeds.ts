/**
 * ============================================
 * 种子数据 (Seeds)
 * ============================================
 *
 * 【职责】
 * - 定义系统初始数据
 * - 提供默认配置项
 * - 创建默认团队
 *
 * 【种子数据分类】
 *
 *   DEFAULT_TEAM (默认团队)
 *   ┌────────────────────────────┐
 *   │ name: 'default'            │
 *   │ description: '默认团队'     │
 *   │ workspaces: ['~/.soloqueue']│
 *   │ isDefault: true            │
 *   └────────────────────────────┘
 *
 *   DEFAULT_CONFIGS (默认配置)
 *   ┌─────────────────────────────────────────┐
 *   │ Category: app                          │
 *   │   ├── app.theme      (string)  "dark"   │
 *   │   └── app.language   (string)  "zh-CN"  │
 *   │                                         │
 *   │ Category: session                      │
 *   │   ├── session.timeout     (number) 3600│
 *   │   ├── session.maxHistory   (number) 1000│
 *   │   └── session.autoSave     (boolean) true│
 *   └─────────────────────────────────────────┘
 *
 * 【初始化时机】
 *
 *   initDb() → runMigrations() → configService.initialize()
 *                                              ↓
 *                                      seedIfEmpty() ← 仅在表为空时插入
 *
 * 【配置值存储格式】
 *
 *   所有值存储为 JSON 字符串:
 *   - 字符串: '"dark"' (带引号)
 *   - 数字: '3600' (不带引号)
 *   - 布尔: 'true' (小写)
 *
 * ============================================
 */

import type { CreateConfigInput } from '../types.js';

export const DEFAULT_TEAM = {
  name: 'default',
  description: '默认团队',
  workspaces: ['~/.soloqueue'],
  isDefault: true,
};

export const DEFAULT_CONFIGS: Omit<CreateConfigInput, 'createdAt' | 'updatedAt'>[] = [
  // App 配置
  { 
    key: 'app.theme', 
    value: '"dark"', 
    type: 'string', 
    category: 'app', 
    description: '应用主题',
    editable: true 
  },
  { 
    key: 'app.language', 
    value: '"zh-CN"', 
    type: 'string', 
    category: 'app', 
    description: '界面语言',
    editable: true 
  },
  
  // Session 配置
  { 
    key: 'session.timeout', 
    value: '3600', 
    type: 'number', 
    category: 'session', 
    description: '会话超时时间(秒)',
    editable: true 
  },
  { 
    key: 'session.maxHistory', 
    value: '1000', 
    type: 'number', 
    category: 'session', 
    description: '最大历史消息数',
    editable: true 
  },
  { 
    key: 'session.autoSave', 
    value: 'true', 
    type: 'boolean', 
    category: 'session', 
    description: '自动保存会话',
    editable: true 
  },
];
