import { useEffect, useRef } from 'react'
import { SessionSidebar } from '@/components/SessionSidebar'
import { ChatMessageView } from '@/components/ChatMessage'
import { ChatInput } from '@/components/ChatInput'
import { useChatStore } from '@/stores/chatStore'
import { useChatStream } from '@/hooks/useChatStream'
import { Sparkles } from 'lucide-react'

export function ChatPage() {
  const { activeSessionId, messages, streaming, sessions } = useChatStore()
  const { send, cancel } = useChatStream()
  const scrollRef = useRef<HTMLDivElement>(null)
  const bottomRef = useRef<HTMLDivElement>(null)

  const currentMessages = messages[activeSessionId || ''] || []
  const noSession = !activeSessionId
  const activeSession = sessions.find((s) => s.id === activeSessionId)

  // Auto-scroll to bottom.
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [currentMessages.length, currentMessages[currentMessages.length - 1]?.segments.length])

  return (
    <div className="flex h-full bg-background">
      {/* Session sidebar */}
      <div className="w-60 shrink-0">
        <SessionSidebar />
      </div>

      {/* Chat area */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Header */}
        <div className="shrink-0 flex items-center justify-between px-4 py-2.5 border-b border-border/50 bg-card/30 backdrop-blur-sm">
          <div className="flex items-center gap-2.5">
            <div className="h-7 w-7 rounded-lg bg-violet-500/10 flex items-center justify-center">
              <Sparkles className="h-3.5 w-3.5 text-violet-500" />
            </div>
            <div>
              <h1 className="text-sm font-semibold text-foreground">
                {activeSession
                  ? activeSession.name || (activeSession.type === 'l1' ? 'L1 Orchestrator' : 'New session')
                  : 'Chat'}
              </h1>
              {activeSession && activeSession.group && (
                <p className="text-[11px] text-muted-foreground/60">{activeSession.group}</p>
              )}
            </div>
          </div>
          {streaming && (
            <div className="flex items-center gap-1.5 text-xs text-violet-500/70">
              <span className="inline-block h-1.5 w-1.5 rounded-full bg-violet-500 animate-pulse" />
              Generating
            </div>
          )}
        </div>

        {/* Messages */}
        <div ref={scrollRef} className="flex-1 overflow-y-auto">
          {noSession ? (
            <div className="flex flex-col items-center justify-center h-full gap-4 text-muted-foreground">
              <div className="h-16 w-16 rounded-2xl bg-muted/50 flex items-center justify-center">
                <Sparkles className="h-8 w-8 text-muted-foreground/30" />
              </div>
              <div className="text-center">
                <p className="text-sm font-medium text-muted-foreground/70">No session selected</p>
                <p className="text-xs text-muted-foreground/40 mt-1">
                  Choose a session from the sidebar or create a new one
                </p>
              </div>
            </div>
          ) : currentMessages.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-full gap-4 text-muted-foreground">
              <div className="h-16 w-16 rounded-2xl bg-violet-500/5 flex items-center justify-center ring-1 ring-violet-500/10">
                <Sparkles className="h-8 w-8 text-violet-500/30" />
              </div>
              <div className="text-center max-w-sm">
                <p className="text-sm font-medium text-foreground/80 mb-1">
                  {activeSession?.type === 'l1'
                    ? 'Ask L1 to coordinate complex tasks'
                    : `Start a new conversation with ${activeSession?.group || 'this agent'}`}
                </p>
                <p className="text-xs text-muted-foreground/40">
                  The agent can browse files, edit code, run commands, and delegate work.
                </p>
              </div>
            </div>
          ) : (
            <div>
              {currentMessages.map((msg) => (
                <ChatMessageView key={msg.id} message={msg} />
              ))}
            </div>
          )}
          <div ref={bottomRef} />
        </div>

        {/* Input */}
        <ChatInput
          onSend={(text) => send(text)}
          onCancel={cancel}
          streaming={streaming}
          disabled={noSession}
        />
      </div>
    </div>
  )
}
