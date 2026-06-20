import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { X, MessageSquare, Send, Loader2, Bot, Info, Activity, User, Users } from 'lucide-react'
import type {
  SimulationPersona,
  SimulationMessage,
  SimulationProgress,
  RelationshipDTO,
} from '@/types'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'

interface AgentDetailPanelProps {
  persona: SimulationPersona
  messages: SimulationMessage[]
  progress: SimulationProgress | null
  relationships?: RelationshipDTO[]
  onClose: () => void
  onInterview: (question: string) => Promise<string>
  status: 'running' | 'completed' | 'idle' | 'pending' | 'failed'
}

// ─── Relationship style mapping (matches SimulationGraph) ────────────────

const RELATION_STYLES: Record<string, { color: string; label: string }> = {
  parent: { color: '#e91e63', label: 'Parent' },
  child: { color: '#e91e63', label: 'Child' },
  sibling: { color: '#9c27b0', label: 'Sibling' },
  spouse: { color: '#e91e63', label: 'Spouse' },
  friend: { color: '#4caf50', label: 'Friend' },
  rival: { color: '#f44336', label: 'Rival' },
  colleague: { color: '#2196f3', label: 'Colleague' },
  mentor: { color: '#ff9800', label: 'Mentor' },
  mentee: { color: '#ff9800', label: 'Mentee' },
  neighbor: { color: '#607d8b', label: 'Neighbor' },
  stranger: { color: '#9e9e9e', label: 'Stranger' },
}

const DEFAULT_STYLE = { color: '#9e9e9e', label: 'Acquaintance' }

export function AgentDetailPanel({
  persona,
  messages,
  progress: _progress,
  relationships = [],
  onClose,
  onInterview,
  status,
}: AgentDetailPanelProps) {
  const [tab, setTab] = useState<string>('info')
  const [question, setQuestion] = useState('')
  const [chatHistory, setChatHistory] = useState<{ q: string; a: string; loading?: boolean }[]>([])
  const [interviewing, setInterviewing] = useState(false)

  const agentMessages = messages.filter((m) => m.agent_id === persona.id).reverse()
  const isCompleted = status === 'completed'
  const isRunning = status === 'running'

  // Filter relationships for this agent (as subject or target)
  const agentRels = relationships.filter(
    (r) => r.subject_id === persona.id || r.target_id === persona.id
  )

  const handleSend = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!question.trim() || interviewing) return

    const q = question.trim()
    setQuestion('')
    setChatHistory((prev) => [...prev, { q, a: '', loading: true }])
    setInterviewing(true)

    try {
      const answer = await onInterview(q)
      setChatHistory((prev) => {
        const copy = [...prev]
        const idx = copy.findIndex((h) => h.q === q && h.loading)
        if (idx >= 0) copy[idx] = { q, a: answer || 'No response.' }
        return copy
      })
    } catch (err: any) {
      setChatHistory((prev) => {
        const copy = [...prev]
        const idx = copy.findIndex((h) => h.q === q && h.loading)
        if (idx >= 0) copy[idx] = { q, a: `Error: ${err.message || 'Interview failed.'}` }
        return copy
      })
    } finally {
      setInterviewing(false)
    }
  }

  const affinityToPercent = (affinity: number) => ((affinity + 1) / 2) * 100

  return (
    <div className="flex flex-col h-full bg-card/30 overflow-hidden">
      {/* Header */}
      <div className="shrink-0 flex items-center justify-between px-4 py-3 border-b border-border bg-card/20">
        <div className="flex items-center gap-3 min-w-0">
          <div className="flex items-center justify-center w-9 h-9 rounded-full bg-primary/10 text-primary text-sm font-bold shrink-0">
            {persona.name.charAt(0)}
          </div>
          <div className="min-w-0">
            <h3 className="font-semibold text-sm text-foreground truncate">{persona.name}</h3>
            <p className="text-[10px] text-muted-foreground truncate">{persona.role}</p>
          </div>
        </div>
        <button
          onClick={onClose}
          className="rounded-lg p-1.5 text-muted-foreground hover:text-foreground hover:bg-muted transition-colors cursor-pointer"
        >
          <X className="h-4 w-4" />
        </button>
      </div>

      {/* Tabs */}
      <Tabs value={tab} onValueChange={setTab} className="flex flex-col flex-1 min-h-0">
        <TabsList className="flex border-b border-border w-full bg-transparent shrink-0">
          <TabsTrigger
            value="info"
            className="flex-1 py-2.5 text-center text-[10px] font-semibold font-mono transition-colors border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/10"
          >
            <Info className="h-3 w-3 mr-1 inline" />
            INFO
          </TabsTrigger>
          <TabsTrigger
            value="relationships"
            className="flex-1 py-2.5 text-center text-[10px] font-semibold font-mono transition-colors border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/10"
          >
            <Users className="h-3 w-3 mr-1 inline" />
            RELATIONS
          </TabsTrigger>
          <TabsTrigger
            value="activity"
            className="flex-1 py-2.5 text-center text-[10px] font-semibold font-mono transition-colors border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/10"
          >
            <Activity className="h-3 w-3 mr-1 inline" />
            ACTIVITY
          </TabsTrigger>
          {(isCompleted || isRunning) && (
            <TabsTrigger
              value="interview"
              className="flex-1 py-2.5 text-center text-[10px] font-semibold font-mono transition-colors border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/10"
            >
              <MessageSquare className="h-3 w-3 mr-1 inline" />
              INTERVIEW
            </TabsTrigger>
          )}
        </TabsList>

        {/* Info Tab */}
        <TabsContent value="info" className="flex-1 overflow-y-auto p-5 space-y-5">
          {/* System Prompt — P0, best real estate */}
          <div>
            <h5 className="text-[11px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-2.5 flex items-center gap-1.5">
              <Bot className="h-3.5 w-3.5" />
              System Prompt
            </h5>
            <div className="rounded-xl border border-border bg-muted/20 p-5 prose prose-sm dark:prose-invert max-w-none">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>
                {persona.system_prompt || '*No system prompt configured.*'}
              </ReactMarkdown>
            </div>
          </div>

          {persona.persona && (
            <div>
              <h5 className="text-[11px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-2.5 flex items-center gap-1.5">
                <User className="h-3.5 w-3.5" />
                Persona
              </h5>
              <div className="rounded-xl border border-border bg-muted/10 p-4 prose prose-sm dark:prose-invert max-w-none">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{persona.persona}</ReactMarkdown>
              </div>
            </div>
          )}

          {persona.bio && (
            <div>
              <h5 className="text-[11px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-2.5">
                Biography
              </h5>
              <div className="rounded-xl border border-border bg-muted/10 p-4 prose prose-sm dark:prose-invert max-w-none">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{persona.bio}</ReactMarkdown>
              </div>
            </div>
          )}

          {persona.goals && persona.goals.length > 0 && (
            <div>
              <h5 className="text-[11px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-2.5">
                Goals
              </h5>
              <ul className="list-disc list-inside space-y-1 text-sm text-foreground/90 leading-relaxed bg-muted/10 p-4 rounded-lg border border-border/40">
                {persona.goals.map((goal, idx) => (
                  <li key={idx}>{goal}</li>
                ))}
              </ul>
            </div>
          )}

          {persona.traits && Object.keys(persona.traits).length > 0 && (
            <div>
              <h5 className="text-[11px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-2.5">
                Traits
              </h5>
              <div className="grid grid-cols-2 gap-3 bg-muted/10 p-4 rounded-lg border border-border/40">
                {Object.entries(persona.traits).map(([k, v]) => (
                  <div key={k} className="text-sm">
                    <span className="font-mono text-muted-foreground mr-1.5">{k}:</span>
                    <span className="text-foreground">{v}</span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Metadata cards — P2, compact */}
          <div className="grid grid-cols-2 gap-3">
            {persona.mbti && (
              <div className="rounded-lg bg-muted/10 border border-border/40 p-3">
                <span className="text-[10px] font-mono text-muted-foreground block">MBTI</span>
                <span className="text-sm font-semibold text-foreground">{persona.mbti}</span>
              </div>
            )}
            {persona.age && (
              <div className="rounded-lg bg-muted/10 border border-border/40 p-3">
                <span className="text-[10px] font-mono text-muted-foreground block">Age</span>
                <span className="text-sm font-semibold text-foreground">{persona.age}</span>
              </div>
            )}
            {persona.country && (
              <div className="rounded-lg bg-muted/10 border border-border/40 p-3">
                <span className="text-[10px] font-mono text-muted-foreground block">Country</span>
                <span className="text-sm font-semibold text-foreground">{persona.country}</span>
              </div>
            )}
            {persona.profession && (
              <div className="rounded-lg bg-muted/10 border border-border/40 p-3">
                <span className="text-[10px] font-mono text-muted-foreground block">
                  Profession
                </span>
                <span className="text-sm font-semibold text-foreground">{persona.profession}</span>
              </div>
            )}
          </div>
        </TabsContent>

        {/* Relationships Tab */}
        <TabsContent value="relationships" className="flex-1 overflow-y-auto p-5 space-y-4">
          {agentRels.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-full text-muted-foreground gap-2">
              <Users className="h-8 w-8 opacity-30" />
              <span className="text-sm">No relationships yet</span>
            </div>
          ) : (
            agentRels.map((rel, idx) => {
              const isSubject = rel.subject_id === persona.id
              const otherName = isSubject ? rel.target_name : rel.subject_name
              const style = RELATION_STYLES[rel.kind] || DEFAULT_STYLE

              return (
                <div
                  key={`${rel.subject_id}-${rel.target_id}-${idx}`}
                  className="rounded-xl border border-border/60 bg-card/30 p-4 space-y-2.5"
                >
                  {/* Relationship header */}
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <div
                        className="w-3 h-3 rounded-full shrink-0"
                        style={{ backgroundColor: style.color }}
                      />
                      <span className="text-sm font-semibold text-foreground">{otherName}</span>
                      <span className="text-[10px] font-mono text-muted-foreground">
                        {isSubject ? '→' : '←'}
                      </span>
                    </div>
                    <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-muted text-muted-foreground border border-border/30">
                      {style.label}
                    </span>
                  </div>

                  {/* Familiarity bar */}
                  <div className="space-y-1">
                    <div className="flex justify-between text-[10px] font-mono text-muted-foreground">
                      <span>Familiarity</span>
                      <span>{(rel.familiarity * 100).toFixed(0)}%</span>
                    </div>
                    <div className="h-1.5 w-full rounded-full bg-muted overflow-hidden">
                      <div
                        className="h-full rounded-full transition-all duration-300"
                        style={{
                          width: `${Math.min(rel.familiarity * 100, 100)}%`,
                          backgroundColor: style.color,
                        }}
                      />
                    </div>
                  </div>

                  {/* Affinity slider */}
                  <div className="space-y-1">
                    <div className="flex justify-between text-[10px] font-mono text-muted-foreground">
                      <span>Affinity</span>
                      <span
                        className={
                          rel.affinity > 0 ? 'text-success' : rel.affinity < 0 ? 'text-error' : ''
                        }
                      >
                        {rel.affinity > 0 ? '+' : ''}
                        {rel.affinity.toFixed(2)}
                      </span>
                    </div>
                    <div className="h-1.5 w-full rounded-full bg-gradient-to-r from-red-400 via-gray-300 to-green-400 relative">
                      <div
                        className="absolute top-[-3px] w-2 h-2 rounded-full bg-foreground border-2 border-background shadow-sm transition-all duration-300"
                        style={{
                          left: `${affinityToPercent(rel.affinity)}%`,
                          transform: 'translateX(-50%)',
                        }}
                      />
                    </div>
                  </div>

                  {/* Tags */}
                  {rel.tags && rel.tags.length > 0 && (
                    <div className="flex flex-wrap gap-1">
                      {rel.tags
                        .filter((t) => t !== rel.kind && t !== '')
                        .map((tag) => (
                          <span
                            key={tag}
                            className="px-1.5 py-0.5 text-[9px] rounded bg-muted/50 text-muted-foreground font-mono border border-border/30"
                          >
                            {tag}
                          </span>
                        ))}
                    </div>
                  )}
                </div>
              )
            })
          )}
        </TabsContent>

        {/* Activity Tab */}
        <TabsContent value="activity" className="flex-1 overflow-y-auto p-5 space-y-3">
          {agentMessages.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-full text-muted-foreground gap-2">
              <Activity className="h-8 w-8 opacity-30" />
              <span className="text-sm">No activity yet</span>
            </div>
          ) : (
            agentMessages.map((msg, i) => (
              <div
                key={`${msg.seq_num}-${i}`}
                className="rounded-xl border border-border/60 bg-card/30 p-4 space-y-2"
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="rounded bg-muted px-2 py-0.5 text-[10px] font-mono text-muted-foreground">
                    R{msg.round} • {msg.type}
                  </span>
                  {msg.to && msg.to !== '*' && (
                    <span className="text-[10px] text-violet-500 font-mono">→ {msg.to}</span>
                  )}
                </div>
                <div className="prose prose-sm dark:prose-invert max-w-none">
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>{msg.content}</ReactMarkdown>
                </div>
                {msg.reasoning && (
                  <details className="mt-2">
                    <summary className="text-[10px] text-muted-foreground cursor-pointer hover:text-foreground font-mono transition-colors">
                      Reasoning
                    </summary>
                    <div className="mt-2 pl-3 border-l-2 border-border prose prose-sm dark:prose-invert max-w-none text-foreground/80">
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>{msg.reasoning}</ReactMarkdown>
                    </div>
                  </details>
                )}
              </div>
            ))
          )}
        </TabsContent>

        {/* Interview Tab */}
        {(isCompleted || isRunning) && (
          <TabsContent value="interview" className="flex-1 flex flex-col min-h-0">
            <div className="flex-1 overflow-y-auto p-5 space-y-4">
              {chatHistory.length === 0 ? (
                <div className="flex flex-col items-center justify-center h-full text-muted-foreground gap-2">
                  <MessageSquare className="h-8 w-8 opacity-30" />
                  <div className="text-sm text-center max-w-[280px] leading-relaxed">
                    Ask {persona.name} about their experience, stance, or interactions during the
                    simulation.
                  </div>
                </div>
              ) : (
                chatHistory.map((chat, idx) => (
                  <div key={idx} className="space-y-3">
                    {/* Question */}
                    <div className="flex justify-end">
                      <div className="rounded-xl bg-primary px-4 py-2.5 text-sm text-primary-foreground max-w-[85%] font-medium">
                        {chat.q}
                      </div>
                    </div>
                    {/* Answer */}
                    <div className="flex justify-start">
                      <div className="rounded-xl bg-muted/70 border border-border px-4 py-3 max-w-[90%]">
                        {chat.loading ? (
                          <div className="flex items-center gap-2 text-muted-foreground text-sm">
                            <Loader2 className="h-4 w-4 animate-spin" />
                            Thinking...
                          </div>
                        ) : (
                          <div className="prose prose-sm dark:prose-invert max-w-none">
                            <ReactMarkdown remarkPlugins={[remarkGfm]}>{chat.a}</ReactMarkdown>
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                ))
              )}
            </div>

            <form
              onSubmit={handleSend}
              className="shrink-0 border-t border-border/50 p-4 bg-card/20 flex gap-2"
            >
              <input
                type="text"
                required
                placeholder={`Ask ${persona.name} a question...`}
                value={question}
                onChange={(e) => setQuestion(e.target.value)}
                className="flex-1 rounded-lg border border-border bg-background px-3 py-2.5 text-sm text-foreground placeholder:text-muted-foreground/50 focus:border-primary focus:ring-1 focus:ring-primary/20 focus:outline-none transition-all"
              />
              <button
                type="submit"
                disabled={interviewing || !question.trim()}
                className="rounded-lg bg-primary hover:bg-primary/90 disabled:bg-primary/50 p-2.5 text-primary-foreground transition-colors cursor-pointer shrink-0 disabled:cursor-not-allowed"
              >
                <Send className="h-4 w-4" />
              </button>
            </form>
          </TabsContent>
        )}
      </Tabs>
    </div>
  )
}
