import { createContext, useContext, useState, type ReactNode } from 'react'

export type Language = 'en' | 'zh'

const translations = {
  en: {
    header: {
      title: 'SoloQueue Status Center',
      desc: 'Local Read-Only Monitoring Dashboard',
    },
    metrics: {
      activeAgents: {
        title: 'Active Agents',
        sub: '{{count}} registered agents in total',
        detail: 'Running: {{running}} · Idle: {{idle}}',
      },
      tokenUsage: {
        title: 'Token Usage',
        sub: 'Total Tokens Consumed',
        detail: 'Input: {{input}} · Output: {{output}}',
      },
      contextOccupancy: {
        title: 'Context Occupancy',
        sub: 'Current Context Window Usage',
      },
      systemErrors: {
        title: 'System Errors',
        sub: 'Agent Execution Error Count',
        detail: 'Current Phase: {{phase}}',
      },
      waiting: 'Waiting for connection...',
      reconnect: 'Auto-reconnect after startup',
      connecting: 'Connecting to server...',
    },
    stream: {
      title: 'Live Inference Stream',
      activeAgents: '· {{count}} active agents',
      iteration: 'Iteration #{{iteration}}',
      thinking: '── Thinking Process ──',
      content: '── Generated Content ──',
      waiting: 'Waiting for inference output...',
    },
    table: {
      title: 'Agent Status Overview',
      totalRegistered: 'Total Registered: {{count}}',
      cols: {
        name: 'Agent Name',
        status: 'Status',
        group: 'Group',
        model: 'Model',
        level: 'Task Level',
        errors: 'Errors',
      },
      badges: {
        leader: 'Leader',
        processing: 'Processing',
        idle: 'Idle',
        stopped: 'Stopped',
      },
      emptyTitle: 'No registered agents yet',
      emptyDesc: 'Registered agents will automatically appear here after the SoloQueue server starts.',
    },
    cron: {
      title: 'Scheduled Tasks',
      tasksCount: '{{count}} task',
      tasksCountPlural: '{{count}} tasks',
      nextRun: 'Next: {{time}}',
    },
    footerInfo: {
      phase: 'Phase: {{phase}}',
      cache: 'Cache: Hit {{hit}} / Miss {{miss}}',
    },
    footer: {
      banner: 'SoloQueue Status Center · Local Access Only · Read-Only Mode',
      summary: 'Agents {{agents}} · Token {{tokens}}',
      disconnected: 'Disconnected',
    },
    connection: {
      connected: 'Connected',
      reconnecting: 'Reconnecting...',
      disconnected: 'Disconnected',
    },
  },
  zh: {
    header: {
      title: 'SoloQueue 运行状态中心',
      desc: '本地只读监控仪表盘',
    },
    metrics: {
      activeAgents: {
        title: '活跃智能体',
        sub: '共注册了 {{count}} 个智能体',
        detail: '运行中: {{running}} · 空闲: {{idle}}',
      },
      tokenUsage: {
        title: 'Token 使用量',
        sub: '累计消耗 Token',
        detail: '输入: {{input}} · 输出: {{output}}',
      },
      contextOccupancy: {
        title: '上下文占用率',
        sub: '当前上下文窗口使用百分比',
      },
      systemErrors: {
        title: '系统错误率',
        sub: '智能体执行错误次数',
        detail: '当前阶段: {{phase}}',
      },
      waiting: '等待连接...',
      reconnect: '启动后将自动重连',
      connecting: '正在连接服务器...',
    },
    stream: {
      title: '实时推演推理流',
      activeAgents: '· {{count}} 个活动智能体',
      iteration: '迭代次数 #{{iteration}}',
      thinking: '── 思考决策过程 ──',
      content: '── 生成响应内容 ──',
      waiting: '等待推理输出中...',
    },
    table: {
      title: '智能体状态大盘',
      totalRegistered: '总计注册: {{count}}',
      cols: {
        name: '智能体名称',
        status: '运行状态',
        group: '工作组',
        model: '使用模型',
        level: '任务级别',
        errors: '异常统计',
      },
      badges: {
        leader: '组长',
        processing: '处理中',
        idle: '空闲',
        stopped: '已停止',
      },
      emptyTitle: '暂无注册智能体',
      emptyDesc: '在 SoloQueue 服务启动后，已注册的智能体将会自动展示在此处。',
    },
    cron: {
      title: '定时触发任务',
      tasksCount: '{{count}} 个任务',
      tasksCountPlural: '{{count}} 个任务',
      nextRun: '下次触发: {{time}}',
    },
    footerInfo: {
      phase: '当前阶段: {{phase}}',
      cache: '缓存: 命中 {{hit}} / 未命中 {{miss}}',
    },
    footer: {
      banner: 'SoloQueue 运行状态中心 · 仅限本地访问 · 只读模式',
      summary: '智能体数 {{agents}} · Token 数 {{tokens}}',
      disconnected: '连接已断开',
    },
    connection: {
      connected: '已连接',
      reconnecting: '重连中...',
      disconnected: '连接已断开',
    },
  },
}

interface LanguageContextValue {
  language: Language
  setLanguage: (lang: Language) => void
  t: (key: string, variables?: Record<string, string | number>) => string
}

const LanguageContext = createContext<LanguageContextValue>({
  language: 'en',
  setLanguage: () => {},
  t: (key) => key,
})

export function LanguageProvider({ children }: { children: ReactNode }) {
  const [language, setLangState] = useState<Language>(() => {
    const stored = localStorage.getItem('soloqueue-language')
    if (stored === 'zh' || stored === 'en') return stored
    return 'en'
  })

  const setLanguage = (lang: Language) => {
    setLangState(lang)
    localStorage.setItem('soloqueue-language', lang)
  }

  const t = (key: string, variables?: Record<string, string | number>): string => {
    const dict = translations[language] || translations['en']
    const parts = key.split('.')
    let current: any = dict

    for (const part of parts) {
      if (current && typeof current === 'object' && part in current) {
        current = current[part]
      } else {
        // Fallback to English dictionary
        let fallbackCurrent: any = translations['en']
        for (const fPart of parts) {
          if (fallbackCurrent && typeof fallbackCurrent === 'object' && fPart in fallbackCurrent) {
            fallbackCurrent = fallbackCurrent[fPart]
          } else {
            fallbackCurrent = key
            break
          }
        }
        current = fallbackCurrent
        break
      }
    }

    if (typeof current !== 'string') {
      return key
    }

    let result = current
    if (variables) {
      Object.entries(variables).forEach(([k, v]) => {
        result = result.replace(new RegExp(`{{${k}}}`, 'g'), String(v))
      })
    }
    return result
  }

  return (
    <LanguageContext.Provider value={{ language, setLanguage, t }}>
      {children}
    </LanguageContext.Provider>
  )
}

export function useTranslation() {
  return useContext(LanguageContext)
}
