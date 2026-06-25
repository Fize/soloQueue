import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

function extractPropertyFromPartialJson(jsonStr: string, propName: string): string | null {
  const regex = new RegExp(`"${propName}"\\s*:\\s*"([^"\\\\]*(?:\\\\.[^"\\\\]*)*)"`)
  const match = jsonStr.match(regex)
  if (match && match[1]) {
    try {
      return JSON.parse(`"${match[1]}"`)
    } catch {
      return match[1]
    }
  }
  return null
}

export function getToolCallSummary(name: string, argsStr: string): string {
  if (!argsStr) return ''
  try {
    const args = JSON.parse(argsStr)

    // Check specific tool names first
    switch (name) {
      case 'Bash':
        return args.command || ''
      case 'Read':
      case 'Write':
      case 'Edit':
      case 'MultiEdit':
      case 'SendFile':
        return args.path || args.TargetFile || ''
      case 'MultiWrite':
        if (args.files && Array.isArray(args.files)) {
          return args.files
            .map((f: { path?: string; TargetFile?: string }) => f.path || f.TargetFile || '')
            .filter(Boolean)
            .join(', ')
        }
        return args.path || ''
      case 'Grep':
        return `${args.query || ''} in ${args.path || args.SearchPath || ''}`
      case 'Glob':
        return args.pattern || args.SearchPath || ''
      case 'WebFetch':
        return args.url || ''
      case 'ImageEdit':
      case 'ImageGenerate':
        return args.prompt || ''
      case 'inspect_agent':
        return args.agent_id || args.name || ''
      case 'KGIndex':
        return `entities/relations`
      case 'MemoryTimeline':
        return `${args.start_date || args.From || ''} to ${args.end_date || args.To || ''}`
      case 'RecallEntity':
        return args.entity || ''
      case 'RecallMemory':
        return args.query || ''
      case 'Remember':
        return args.content || ''
      case 'schedule_task':
        return args.instruction || args.Prompt || ''
      case 'WebSearch':
        return args.query || ''
      case 'ConnectEntities':
        return `${args.source || ''} -> ${args.target || ''}`
    }

    // General fallback
    if (args && typeof args === 'object') {
      const keys = [
        'command',
        'path',
        'TargetFile',
        'SearchPath',
        'query',
        'url',
        'pattern',
        'prompt',
        'entity',
        'source',
        'text',
      ]
      for (const key of keys) {
        if (typeof args[key] === 'string' && args[key]) {
          return args[key]
        }
      }
    }
  } catch {
    // regex fallback for partial json
    const keysToCheck =
      name === 'Bash'
        ? ['command']
        : ['Read', 'Write', 'Edit', 'MultiEdit', 'SendFile'].includes(name)
          ? ['path', 'TargetFile']
          : name === 'Grep'
            ? ['query', 'path', 'SearchPath']
            : name === 'Glob'
              ? ['pattern', 'SearchPath']
              : name === 'WebFetch'
                ? ['url']
                : ['ImageEdit', 'ImageGenerate'].includes(name)
                  ? ['prompt']
                  : ['WebSearch', 'RecallMemory'].includes(name)
                    ? ['query']
                    : [
                        'command',
                        'path',
                        'TargetFile',
                        'SearchPath',
                        'query',
                        'url',
                        'pattern',
                        'prompt',
                        'entity',
                        'source',
                        'text',
                      ]

    for (const key of keysToCheck) {
      const val = extractPropertyFromPartialJson(argsStr, key)
      if (val) {
        if (name === 'Grep' && key === 'query') {
          const pathVal =
            extractPropertyFromPartialJson(argsStr, 'path') ||
            extractPropertyFromPartialJson(argsStr, 'SearchPath') ||
            ''
          return `${val} in ${pathVal}`
        }
        return val
      }
    }
  }
  return ''
}

export function formatToolCallHeader(name: string, argsStr: string): string {
  const summary = getToolCallSummary(name, argsStr)

  if (summary) {
    return `${name}   ${summary}`
  } else {
    return `${name}`
  }
}
