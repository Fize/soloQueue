// ─── Tool & Skill Types ────────────────────────────────────────────────────

export interface ToolInfo {
  name: string
  description: string
  parameters: Record<string, unknown> | null
}

export interface ToolListResponse {
  tools: ToolInfo[]
  total: number
}

export interface SkillInfo {
  id: string
  name: string
  description: string
  when_to_use: string
  user_invocable: boolean
  disable_model_invocation: boolean
  context: string
  agent: string
  file_path: string
  allowed_tools: string[]
  triggers?: string[]
  body?: string
  required_env?: string[]
}

export interface SkillListResponse {
  skills: SkillInfo[]
  total: number
}

// ─── MCP Types ─────────────────────────────────────────────────────────────────

export interface MCPServerConfig {
  name: string
  command: string
  args: string[]
  env?: Record<string, string>
  transport: string
  enabled: boolean
}

export interface MCPServerWire {
  command: string
  args: string[]
  env?: Record<string, string>
  transport?: string
  enabled?: boolean
}

export interface MCPConfig {
  mcpServers: Record<string, MCPServerWire>
}

export interface MCPServerInfo {
  name: string
  source: 'builtin' | 'external'
  command?: string
}

export interface MCPAvailableResponse {
  servers: MCPServerInfo[]
}

// ─── File Types ─────────────────────────────────────────────────────────────────

export interface FileInfo {
  name: string
  path: string
  size: number
  isDir: boolean
  ext: string
  modTime: string
}
