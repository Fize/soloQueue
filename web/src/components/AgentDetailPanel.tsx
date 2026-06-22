import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {
  X,
  MessageSquare,
  Send,
  Loader2,
  Bot,
  Activity,
  Users,
  Info,
  Award,
  User,
} from 'lucide-react'
import type {
  SimulationPersona,
  SimulationMessage,
  SimulationProgress,
  RelationshipDTO,
} from '@/types'
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from '@/components/ui/tooltip'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

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
      <div className="shrink-0 flex items-center justify-between px-4 py-3 border-b border-border bg-card/25">
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

      {/* Upper Pane - Tabs */}
      <Tabs defaultValue="profile" className="flex-1 flex flex-col min-h-0 overflow-hidden">
        <TabsList className="flex border-b border-border w-full bg-transparent shrink-0">
          <TabsTrigger
            value="profile"
            className="flex-1 py-2 text-center text-xs font-semibold font-mono border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/20"
          >
            PROFILE
          </TabsTrigger>
          <TabsTrigger
            value="logs"
            className="flex-1 py-2 text-center text-xs font-semibold font-mono border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/20"
          >
            ACTIVE LOGS
          </TabsTrigger>
        </TabsList>

        <TabsContent
          value="profile"
          className="flex-1 overflow-y-auto p-6 space-y-6 min-h-0 focus-visible:outline-none"
        >
          {/* Metadata badges: Age, Gender, MBTI, Country, Profession */}
          <div className="flex flex-wrap gap-1.5 text-[10px] font-mono">
            {persona.age && (
              <span className="px-2 py-1 rounded bg-muted text-foreground">{persona.age} yrs</span>
            )}
            {persona.gender && (
              <span className="px-2 py-1 rounded bg-muted text-foreground uppercase">
                {persona.gender === 'male'
                  ? 'Male'
                  : persona.gender === 'female'
                    ? 'Female'
                    : persona.gender}
              </span>
            )}
            {persona.mbti && (
              <span className="px-2 py-1 rounded bg-primary/10 text-primary font-bold">
                {persona.mbti}
              </span>
            )}
            {persona.country && (
              <span className="px-2 py-1 rounded bg-muted text-foreground">
                📍 {persona.country}
              </span>
            )}
            {persona.profession && (
              <span className="px-2 py-1 rounded bg-muted text-foreground">
                💼 {persona.profession}
              </span>
            )}
          </div>

          {/* Bio */}
          {persona.bio && (
            <div className="space-y-2">
              <h4 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono flex items-center gap-1.5 border-b border-border/40 pb-1.5">
                <User className="h-3.5 w-3.5" />
                Biography
              </h4>
              <p className="text-xs text-foreground/90 leading-relaxed bg-muted/10 p-3 rounded-lg border border-border/40 italic">
                &ldquo;{persona.bio}&rdquo;
              </p>
            </div>
          )}

          {/* Detailed Persona Description */}
          {persona.persona && (
            <div className="space-y-2">
              <h4 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono flex items-center gap-1.5 border-b border-border/40 pb-1.5">
                <Bot className="h-3.5 w-3.5" />
                Detailed Persona
              </h4>
              <div className="rounded-xl border border-border bg-muted/10 p-4 prose prose-sm dark:prose-invert max-w-none text-xs text-foreground/90 max-h-48 overflow-y-auto">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{persona.persona}</ReactMarkdown>
              </div>
            </div>
          )}

          {/* Goals */}
          {persona.goals && persona.goals.length > 0 && (
            <div className="space-y-2">
              <h4 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono flex items-center gap-1.5 border-b border-border/40 pb-1.5">
                <Award className="h-3.5 w-3.5" />
                Goals
              </h4>
              <ul className="list-disc list-inside space-y-1 text-xs text-foreground/90 pl-1">
                {persona.goals.map((g, i) => (
                  <li key={i} className="leading-relaxed select-text">
                    {g}
                  </li>
                ))}
              </ul>
            </div>
          )}

          {/* Traits */}
          {persona.traits && Object.keys(persona.traits).length > 0 && (
            <div className="space-y-2">
              <h4 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono flex items-center gap-1.5 border-b border-border/40 pb-1.5">
                <Info className="h-3.5 w-3.5" />
                Traits
              </h4>
              <div className="flex flex-wrap gap-1.5">
                {Object.entries(persona.traits)
                  .filter(([k]) => k !== 'role_type' && !k.startsWith('stance:'))
                  .map(([k, v]) => (
                    <span
                      key={k}
                      className="px-2 py-1 rounded bg-muted/50 border border-border/40 text-[10px] font-mono text-foreground/80"
                    >
                      {k}={v}
                    </span>
                  ))}
              </div>
            </div>
          )}

          {/* Relationships Row */}
          <div className="space-y-4">
            <h4 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono flex items-center gap-1.5 border-b border-border/40 pb-1.5">
              <Users className="h-3.5 w-3.5" />
              Relationships
            </h4>
            {agentRels.length === 0 ? (
              <div className="text-xs text-muted-foreground italic pl-1">
                No relationships formed.
              </div>
            ) : (
              <TooltipProvider>
                <div className="flex flex-wrap gap-2">
                  {agentRels.map((rel, idx) => {
                    const isSubject = rel.subject_id === persona.id
                    const otherName = isSubject ? rel.target_name : rel.subject_name
                    const style = RELATION_STYLES[rel.kind] || DEFAULT_STYLE
                    return (
                      <Tooltip key={`${rel.subject_id}-${rel.target_id}-${idx}`}>
                        <TooltipTrigger className="flex items-center gap-1.5 px-3 py-1.5 rounded-full bg-muted/30 border border-border/60 hover:bg-muted/50 transition-colors cursor-pointer select-none">
                          <span
                            className="w-2.5 h-2.5 rounded-full"
                            style={{ backgroundColor: style.color }}
                          />
                          <span className="text-xs font-medium text-foreground">{otherName}</span>
                          <span className="text-[10px] text-muted-foreground font-mono">
                            ({style.label})
                          </span>
                        </TooltipTrigger>
                        <TooltipContent
                          side="top"
                          className="flex flex-col gap-1.5 p-3 min-w-48 bg-card border border-border shadow-md"
                        >
                          <div className="font-semibold text-foreground text-xs">{otherName}</div>
                          <div className="text-[10px] text-muted-foreground flex items-center gap-1.5">
                            <span>Kind:</span>
                            <span className="font-semibold text-foreground px-1 py-0.2 rounded bg-muted border border-border/30">
                              {style.label}
                            </span>
                            <span className="text-[9px] font-mono text-muted-foreground">
                              {isSubject ? '(outgoing)' : '(incoming)'}
                            </span>
                          </div>
                          <div className="space-y-1">
                            <div className="flex justify-between text-[9px] font-mono text-muted-foreground">
                              <span>Familiarity</span>
                              <span>{(rel.familiarity * 100).toFixed(0)}%</span>
                            </div>
                            <div className="h-1 w-full rounded-full bg-muted overflow-hidden">
                              <div
                                className="h-full rounded-full"
                                style={{
                                  width: `${Math.min(rel.familiarity * 100, 100)}%`,
                                  backgroundColor: style.color,
                                }}
                              />
                            </div>
                          </div>
                          <div className="space-y-1">
                            <div className="flex justify-between text-[9px] font-mono text-muted-foreground">
                              <span>Affinity</span>
                              <span
                                className={
                                  rel.affinity > 0
                                    ? 'text-success'
                                    : rel.affinity < 0
                                      ? 'text-error'
                                      : ''
                                }
                              >
                                {rel.affinity > 0 ? '+' : ''}
                                {rel.affinity.toFixed(2)}
                              </span>
                            </div>
                            <div className="h-1 w-full rounded-full bg-gradient-to-r from-red-400 via-gray-300 to-green-400 relative">
                              <div
                                className="absolute top-[-3px] w-1.5 h-1.5 rounded-full bg-foreground border border-background shadow-sm transition-all duration-300"
                                style={{
                                  left: `${affinityToPercent(rel.affinity)}%`,
                                  transform: 'translateX(-50%)',
                                }}
                              />
                            </div>
                          </div>
                          {rel.tags &&
                            rel.tags.filter((t) => t !== rel.kind && t !== '').length > 0 && (
                              <div className="flex flex-wrap gap-0.5 border-t border-border/30 pt-1.5 mt-0.5">
                                {rel.tags
                                  .filter((t) => t !== rel.kind && t !== '')
                                  .map((tag) => (
                                    <span
                                      key={tag}
                                      className="px-1 py-0.2 text-[8px] rounded bg-muted text-muted-foreground font-mono border border-border/30"
                                    >
                                      {tag}
                                    </span>
                                  ))}
                              </div>
                            )}
                        </TooltipContent>
                      </Tooltip>
                    )
                  })}
                </div>
              </TooltipProvider>
            )}
          </div>
        </TabsContent>

        <TabsContent
          value="logs"
          className="flex-1 overflow-y-auto p-6 space-y-4 min-h-0 focus-visible:outline-none"
        >
          {/* Activity Log */}
          <div className="space-y-4">
            <h4 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono flex items-center gap-1.5 border-b border-border/40 pb-1.5">
              <Activity className="h-3.5 w-3.5" />
              Activity Log
            </h4>
            {agentMessages.length === 0 ? (
              <div className="flex flex-col items-center justify-center p-6 text-muted-foreground gap-2 border border-dashed rounded-xl">
                <Activity className="h-6 w-6 opacity-30" />
                <span className="text-xs">No activity logged in this simulation.</span>
              </div>
            ) : (
              <div className="space-y-3">
                {agentMessages.map((msg, i) => (
                  <div
                    key={`${msg.seq_num}-${i}`}
                    className="rounded-xl border border-border/60 bg-card/30 p-4 space-y-2 text-xs"
                  >
                    <div className="flex items-center justify-between gap-2 border-b border-border/30 pb-1.5">
                      <span className="rounded bg-muted px-2 py-0.5 text-[9px] font-mono text-muted-foreground">
                        R{msg.round} • {msg.type}
                      </span>
                      {msg.to && msg.to !== '*' && (
                        <span className="text-[9px] text-violet-500 font-mono">→ {msg.to}</span>
                      )}
                    </div>
                    <div className="prose prose-sm dark:prose-invert max-w-none text-foreground/90 select-text">
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>{msg.content}</ReactMarkdown>
                    </div>
                    {msg.reasoning && (
                      <details className="mt-2">
                        <summary className="text-[9px] text-muted-foreground cursor-pointer hover:text-foreground font-mono transition-colors">
                          Reasoning
                        </summary>
                        <div className="mt-2 pl-3 border-l-2 border-border prose prose-sm dark:prose-invert max-w-none text-muted-foreground/80 leading-relaxed italic select-text">
                          <ReactMarkdown remarkPlugins={[remarkGfm]}>{msg.reasoning}</ReactMarkdown>
                        </div>
                      </details>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        </TabsContent>
      </Tabs>

      {/* Lower Pane - Interview */}
      {isCompleted || isRunning ? (
        <div className="h-[320px] border-t border-border shrink-0 flex flex-col bg-card/25 min-h-0">
          <div className="shrink-0 flex items-center justify-between px-5 py-2 border-b border-border bg-card/20">
            <div className="flex items-center gap-2">
              <MessageSquare className="h-4 w-4 text-primary" />
              <span className="text-xs font-bold font-mono tracking-wide uppercase text-foreground">
                In-Character Interview
              </span>
            </div>
            <span className="text-[10px] text-muted-foreground">
              Ask agent about their stance or thoughts
            </span>
          </div>

          {/* Interview messages */}
          <div className="flex-1 overflow-y-auto p-5 space-y-4 min-h-0 select-text">
            {chatHistory.length === 0 ? (
              <div className="flex flex-col items-center justify-center h-full text-muted-foreground gap-2">
                <MessageSquare className="h-8 w-8 opacity-20" />
                <div className="text-xs text-center max-w-[320px] leading-relaxed">
                  Start an interview thread with {persona.name} regarding the simulation events.
                </div>
              </div>
            ) : (
              chatHistory.map((chat, idx) => (
                <div key={idx} className="space-y-3">
                  {/* Question */}
                  <div className="flex justify-end">
                    <div className="rounded-xl bg-primary px-3 py-1.5 text-xs text-primary-foreground max-w-[85%] font-medium">
                      {chat.q}
                    </div>
                  </div>
                  {/* Answer */}
                  <div className="flex justify-start">
                    <div className="rounded-xl bg-muted/70 border border-border px-3 py-2 max-w-[90%] text-xs">
                      {chat.loading ? (
                        <div className="flex items-center gap-2 text-muted-foreground">
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          Thinking...
                        </div>
                      ) : (
                        <div className="prose prose-sm dark:prose-invert max-w-none text-xs leading-relaxed text-foreground select-text">
                          <ReactMarkdown remarkPlugins={[remarkGfm]}>{chat.a}</ReactMarkdown>
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              ))
            )}
          </div>

          {/* Form */}
          <form
            onSubmit={handleSend}
            className="shrink-0 border-t border-border/40 p-4 bg-card/40 flex gap-2"
          >
            <input
              type="text"
              required
              placeholder={`Ask ${persona.name} a question...`}
              value={question}
              onChange={(e) => setQuestion(e.target.value)}
              className="flex-1 rounded-lg border border-border bg-background px-3 py-2 text-xs text-foreground placeholder:text-muted-foreground/50 focus:border-primary focus:ring-1 focus:ring-primary/20 focus:outline-none transition-all"
            />
            <button
              type="submit"
              disabled={interviewing || !question.trim()}
              className="rounded-lg bg-primary hover:bg-primary/90 disabled:bg-primary/50 p-2 text-primary-foreground transition-colors cursor-pointer shrink-0 disabled:cursor-not-allowed"
            >
              <Send className="h-4 w-4" />
            </button>
          </form>
        </div>
      ) : (
        <div className="h-[120px] border-t border-border shrink-0 flex flex-col bg-card/25 items-center justify-center p-4">
          <span className="text-xs text-muted-foreground text-center">
            Interviewing is only available while the simulation is running or completed.
          </span>
        </div>
      )}
    </div>
  )
}
