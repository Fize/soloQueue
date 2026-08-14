import { createContext, useContext, useState, type ReactNode } from 'react'

export type Language = 'en' | 'zh'

// Architectural Decision: Custom lightweight i18n context.
// Avoids external dependencies (e.g. i18next) to minimize embedded binary footprint in Go //go:embed dist.
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
      toolCall: '── Tool Call ──',
      toolDone: 'Done',
      toolError: 'Error',
      toolDuration: '{{ms}} ms',
      waiting: 'Waiting for inference output...',
      errors: 'Errors',
    },
    table: {
      title: 'Agent Status Overview',
      totalRegistered: 'Total Registered: {{count}}',
      cols: {
        name: 'Agent Name',
        status: 'Status',
        group: 'Group',
        model: 'Model',
        taskType: 'Task Type',
        level: 'Task Level',
        lastLevel: 'Last level: {{level}}',
        iteration: 'Iteration',
        errors: 'Errors',
      },
      badges: {
        leader: 'Leader',
        processing: 'Processing',
        idle: 'Idle',
        stopped: 'Stopped',
        stopping: 'Stopping',
      },
      emptyTitle: 'No registered agents yet',
      emptyDesc: 'Registered agents will automatically appear here after the SoloQueue server starts.',
      groups: {
        all: 'All Agents',
        ungrouped: 'Ungrouped',
        supervisor: 'Supervisor: {{name}}',
      },
    },
    cron: {
      title: 'Scheduled Tasks',
      tasksCount: '{{count}} task',
      tasksCountPlural: '{{count}} tasks',
      nextRun: 'Next: {{time}}',
      statusLabel: 'Status: {{status}}',
      emptyTitle: 'No scheduled tasks',
      emptyDesc: 'Configured cron tasks will appear here.',
      history: 'History',
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
    tokenStats: {
      title: 'Token Usage Statistics',
      subtitle: 'Read-only usage, reliability, and activity details',
      loading: 'Loading token statistics...',
      refreshing: 'Refreshing...',
      refresh: 'Refresh',
      retry: 'Retry',
      loadError: 'Usage statistics could not be loaded.',
      refreshError: 'Refresh failed. The last successful snapshot remains visible.',
      partialError: 'Some detail sections are temporarily unavailable. Overview data is still current.',
      noData: 'No token usage data yet',
      noDataForSelection: 'No data for this selection.',
      sectionUnavailable: 'Unavailable for this refresh.',
      legacyOnly: 'Legacy records include tokens and models only. Reliability, latency, cache, reasoning, task-type, and origin details are unavailable.',
      mixedCoverage: '{{count}} legacy records have unavailable historical details.',
      currentCoverage: 'All records in this range use the current telemetry format.',
      legacy: 'Legacy',
      unavailable: 'Unavailable',
      dbUnavailable: 'Database not available',
      filtersTitle: 'Usage filters',
      resetFilters: 'Reset filters',
      team: 'Team',
      origin: 'Origin',
      usageType: 'Usage type',
      taskType: 'Task type',
      model: 'Model',
      status: 'Status',
      allTeams: 'All teams',
      allOrigins: 'All origins',
      allUsageTypes: 'All usage types',
      allTaskTypes: 'All task types',
      allModels: 'All models',
      allStatuses: 'All statuses',
      statusSuccess: 'Success',
      statusError: 'Error',
      statusCancelled: 'Cancelled',
      statusTimeout: 'Timeout',
      summaryTitle: 'Summary',
      summaryTotal: 'Total',
      summaryCacheHit: 'Cache Hit Rate',
      successRate: 'Success Rate',
      reasoningTokens: 'Reasoning Tokens',
      previousPeriod: 'vs previous period',
      preset24h: 'Last 24h',
      preset7d: 'Last 7d',
      preset30d: 'Last 30d',
      requests: 'Requests',
      p95Latency: 'P95 Latency',
      coverageTitle: 'Data Coverage',
      coverageValue: '{{known}} of {{applicable}} applicable',
      coverageUnavailable: 'Unavailable · 0 of {{applicable}} collected',
      reliability: 'Reliability',
      latency: 'Latency',
      cache: 'Cache',
      reasoning: 'Reasoning',
      trendTitle: 'Trend',
      trendMetric: 'Trend metric',
      trendtokens: 'Tokens',
      trendcalls: 'Calls',
      trenderrors: 'Errors',
      trendlatency: 'Latency',
      insightsTitle: 'Insights',
      errorsDetectedTitle: 'Failed calls detected',
      errorsDetectedDetail: '{{count}} calls failed or timed out in this period.',
      tokenChangeTitle: 'Token usage changed significantly',
      tokenChangeDetail: 'Token usage changed by {{change}}% compared with the previous period.',
      breakdownsTitle: 'Breakdowns',
      byModel: 'By model',
      byUsageType: 'By usage type',
      byTeam: 'By team',
      byTaskType: 'By task type',
      byOrigin: 'By origin',
      byStatus: 'By status',
      activityTitle: 'Activity · Last 365 days',
      activitySummary: '{{days}} active days · {{tokens}} tokens',
      activityAria: 'Daily token activity heatmap',
      activityDay: '{{date}}: {{requests}} requests, {{tokens}} tokens',
      noActivity: 'No activity in this period.',
      eventsTitle: 'Recent Calls',
      eventsCount: '{{count}} loaded',
      noEvents: 'No calls for this selection.',
      time: 'Time',
      context: 'Context',
      tokens: 'Tokens',
      duration: 'Duration',
      loadMore: 'Load more',
      loadingMore: 'Loading...',
      retryLoadMore: 'Retry load more',
      paginationError: 'More calls could not be loaded. Existing rows remain visible.',
      updated: 'Updated',
    },
    modal: {
      state: 'State',
      model: 'Model',
      group: 'Group',
      level: 'Task Level',
      iteration: 'Iteration',
      errors: 'Errors',
      streamTitle: 'Live Stream',
      idle: 'Agent is currently idle',
      noStream: 'No stream data available',
      lastError: 'Last Error',
      lastLevel: 'Last level: {{level}}',
      close: 'Close',
    },
    notification: {
      info: 'Info',
      success: 'Success',
      warning: 'Warning',
      error: 'Error',
    },
    cronHistory: {
      title: 'Execution History — {{name}}',
      time: 'Time',
      status: 'Status',
      duration: 'Duration',
      summary: 'Summary',
      model: 'Model',
      empty: 'No execution records yet',
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
      toolCall: '── 工具调用 ──',
      toolDone: '完成',
      toolError: '错误',
      toolDuration: '{{ms}} 毫秒',
      waiting: '等待推理输出中...',
      errors: '错误信息',
    },
    table: {
      title: '智能体状态大盘',
      totalRegistered: '总计注册: {{count}}',
      cols: {
        name: '智能体名称',
        status: '运行状态',
        group: '工作组',
        model: '使用模型',
        taskType: '任务类型',
        level: '任务级别',
        lastLevel: '上次级别: {{level}}',
        iteration: '迭代次数',
        errors: '异常统计',
      },
      badges: {
        leader: '组长',
        processing: '处理中',
        idle: '空闲',
        stopped: '已停止',
        stopping: '停止中',
      },
      emptyTitle: '暂无注册智能体',
      emptyDesc: '在 SoloQueue 服务启动后，已注册的智能体将会自动展示在此处。',
      groups: {
        all: '全部智能体',
        ungrouped: '未分组',
        supervisor: '管理者: {{name}}',
      },
    },
    cron: {
      title: '定时触发任务',
      tasksCount: '{{count}} 个任务',
      tasksCountPlural: '{{count}} 个任务',
      nextRun: '下次触发: {{time}}',
      statusLabel: '状态: {{status}}',
      emptyTitle: '暂无定时任务',
      emptyDesc: '配置的定时任务将会显示在此处。',
      history: '历史',
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
    tokenStats: {
      title: 'Token 使用统计',
      subtitle: '只读查看用量、可靠性与活跃度明细',
      loading: '正在加载 Token 统计数据...',
      refreshing: '正在刷新...',
      refresh: '刷新',
      retry: '重试',
      loadError: '无法加载使用统计。',
      refreshError: '刷新失败，页面继续显示最近一次成功的数据。',
      partialError: '部分明细暂时不可用，概览数据仍可正常查看。',
      noData: '暂无 Token 使用数据',
      noDataForSelection: '当前筛选条件下暂无数据。',
      sectionUnavailable: '本次刷新暂时不可用。',
      legacyOnly: '旧版记录仅包含 Token 与模型；可靠性、延迟、缓存、推理、任务类型和来源明细不可用。',
      mixedCoverage: '{{count}} 条旧版记录的历史明细不可用。',
      currentCoverage: '当前范围内的记录均使用新版遥测格式。',
      legacy: '旧版',
      unavailable: '不可用',
      dbUnavailable: '数据库不可用',
      filtersTitle: '用量筛选',
      resetFilters: '重置筛选',
      team: '团队',
      origin: '来源',
      usageType: '用量类型',
      taskType: '任务类型',
      model: '模型',
      status: '状态',
      allTeams: '全部团队',
      allOrigins: '全部来源',
      allUsageTypes: '全部用量类型',
      allTaskTypes: '全部任务类型',
      allModels: '全部模型',
      allStatuses: '全部状态',
      statusSuccess: '成功',
      statusError: '错误',
      statusCancelled: '已取消',
      statusTimeout: '超时',
      summaryTitle: '概览',
      summaryTotal: '总计',
      summaryCacheHit: '缓存命中率',
      successRate: '成功率',
      reasoningTokens: '推理 Token',
      previousPeriod: '较上一周期',
      preset24h: '最近 24 小时',
      preset7d: '最近 7 天',
      preset30d: '最近 30 天',
      requests: '请求数',
      p95Latency: 'P95 延迟',
      coverageTitle: '数据覆盖率',
      coverageValue: '{{applicable}} 条适用记录中已采集 {{known}} 条',
      coverageUnavailable: '不可用 · {{applicable}} 条适用记录中采集 0 条',
      reliability: '可靠性',
      latency: '延迟',
      cache: '缓存',
      reasoning: '推理',
      trendTitle: '趋势',
      trendMetric: '趋势指标',
      trendtokens: 'Token',
      trendcalls: '调用数',
      trenderrors: '错误数',
      trendlatency: '延迟',
      insightsTitle: '洞察',
      errorsDetectedTitle: '检测到失败调用',
      errorsDetectedDetail: '当前周期有 {{count}} 次调用失败或超时。',
      tokenChangeTitle: 'Token 用量显著变化',
      tokenChangeDetail: 'Token 用量较上一周期变化 {{change}}%。',
      breakdownsTitle: '维度分布',
      byModel: '按模型',
      byUsageType: '按用量类型',
      byTeam: '按团队',
      byTaskType: '按任务类型',
      byOrigin: '按来源',
      byStatus: '按状态',
      activityTitle: '活跃度 · 最近 365 天',
      activitySummary: '{{days}} 个活跃日 · {{tokens}} Token',
      activityAria: '每日 Token 活跃度热力图',
      activityDay: '{{date}}：{{requests}} 次请求，{{tokens}} Token',
      noActivity: '当前周期暂无活跃记录。',
      eventsTitle: '最近调用',
      eventsCount: '已加载 {{count}} 条',
      noEvents: '当前筛选条件下暂无调用。',
      time: '时间',
      context: '上下文',
      tokens: 'Token',
      duration: '耗时',
      loadMore: '加载更多',
      loadingMore: '加载中...',
      retryLoadMore: '重试加载更多',
      paginationError: '无法加载更多调用，现有记录仍保留。',
      updated: '更新于',
    },
    modal: {
      state: '状态',
      model: '模型',
      group: '工作组',
      level: '任务级别',
      iteration: '当前迭代',
      errors: '错误数',
      streamTitle: '实时流',
      idle: '智能体当前处于空闲状态',
      noStream: '暂无流数据',
      lastError: '最近错误',
      lastLevel: '上次级别: {{level}}',
      close: '关闭',
    },
    notification: {
      info: '信息',
      success: '成功',
      warning: '警告',
      error: '错误',
    },
    cronHistory: {
      title: '执行历史 — {{name}}',
      time: '时间',
      status: '状态',
      duration: '耗时',
      summary: '摘要',
      model: '模型',
      empty: '暂无执行记录',
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
    return 'zh'
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
