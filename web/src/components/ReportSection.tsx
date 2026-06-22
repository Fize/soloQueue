import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { FileText, Send, Loader2, MessageSquare, Bot } from 'lucide-react'

interface ReportSectionProps {
  report: string
  topic: string
  onInterview: (question: string) => Promise<string>
}

export function ReportSection({ report, topic, onInterview }: ReportSectionProps) {
  const [showInterview, setShowInterview] = useState(false)
  const [question, setQuestion] = useState('')
  const [chatHistory, setChatHistory] = useState<{ q: string; a: string; loading?: boolean }[]>([])
  const [interviewing, setInterviewing] = useState(false)

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

  return (
    <div className="flex flex-col h-full bg-card/30 border-l border-border overflow-hidden">
      {/* Header */}
      <div className="shrink-0 flex items-center justify-between px-5 py-3.5 border-b border-border bg-card/20">
        <div className="flex items-center gap-3 min-w-0">
          <div className="flex items-center justify-center w-10 h-10 rounded-full bg-primary/10 text-primary shrink-0">
            <FileText className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <h3 className="font-semibold text-sm text-foreground truncate">Final Report</h3>
            <p className="text-[11px] text-muted-foreground truncate">{topic}</p>
          </div>
        </div>
        <button
          onClick={() => setShowInterview(!showInterview)}
          className={`flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-[11px] font-semibold font-mono transition-all cursor-pointer shrink-0 ${
            showInterview
              ? 'border-primary/50 bg-primary/5 text-primary'
              : 'border-border/60 bg-muted/30 text-muted-foreground hover:text-foreground hover:border-border'
          }`}
        >
          <MessageSquare className="h-3.5 w-3.5" />
          {showInterview ? 'Hide Chat' : 'Ask Analyst'}
        </button>
      </div>

      <div className="flex-1 flex flex-col min-h-0 overflow-hidden">
        {showInterview ? (
          /* Interview mode */
          <div className="flex-1 flex flex-col min-h-0">
            <div className="flex-1 overflow-y-auto p-5 space-y-4">
              {chatHistory.length === 0 ? (
                <div className="flex flex-col items-center justify-center h-full text-muted-foreground gap-2">
                  <Bot className="h-10 w-10 opacity-30" />
                  <div className="text-sm text-center max-w-[320px] leading-relaxed">
                    Ask the Report Analyst about the simulation findings, data points, or
                    conclusions in the report.
                  </div>
                </div>
              ) : (
                chatHistory.map((chat, idx) => (
                  <div key={idx} className="space-y-3">
                    <div className="flex justify-end">
                      <div className="rounded-xl bg-primary px-4 py-2.5 text-sm text-primary-foreground max-w-[85%] font-medium">
                        {chat.q}
                      </div>
                    </div>
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
                placeholder="Ask about the report..."
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
          </div>
        ) : (
          /* Report view — P0, generous spacing */
          <div className="flex-1 overflow-y-auto p-5 lg:p-6">
            <div className="prose prose-sm dark:prose-invert max-w-none">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{report}</ReactMarkdown>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
