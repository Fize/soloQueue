import { type KeyboardEvent, useRef, useEffect, useCallback } from 'react'
import { ArrowUp, StopCircle } from 'lucide-react'

export interface ChatInputProps {
  onSend: (text: string) => void
  onCancel: () => void
  streaming: boolean
  disabled: boolean
}

export function ChatInput({ onSend, onCancel, streaming, disabled }: ChatInputProps) {
  const inputRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    if (!streaming) {
      inputRef.current?.focus()
    }
  }, [streaming])

  const handleSubmit = useCallback(() => {
    const text = inputRef.current?.value.trim() || ''
    if (!text || streaming || disabled) return
    onSend(text)
    if (inputRef.current) inputRef.current.value = ''
    // Reset height
    if (inputRef.current) inputRef.current.style.height = 'auto'
  }, [streaming, disabled, onSend])

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSubmit()
    }
  }

  const autoResize = () => {
    const el = inputRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 200) + 'px'
  }

  const placeholderText = disabled
    ? 'Select a session to begin...'
    : 'Ask anything — Enter to send, Shift+Enter for newline'

  return (
    <div className="border-t border-border/50 bg-gradient-to-t from-card to-card/80 backdrop-blur-sm">
      <div className="mx-auto max-w-3xl px-4 py-4">
        <div className="relative flex items-end rounded-2xl border border-border/60 bg-background shadow-sm transition-shadow focus-within:shadow-md focus-within:border-primary/30 focus-within:ring-2 focus-within:ring-primary/5">
          <textarea
            ref={inputRef}
            className="flex-1 resize-none bg-transparent px-4 py-3 text-[15px] leading-relaxed text-foreground placeholder:text-muted-foreground/50 focus:outline-none min-h-[48px] max-h-[200px]"
            placeholder={placeholderText}
            rows={1}
            disabled={streaming || disabled}
            onKeyDown={handleKeyDown}
            onInput={autoResize}
          />
          <div className="shrink-0 flex items-center gap-1 pr-2 pb-2">
            {streaming ? (
              <button
                onClick={onCancel}
                className="flex items-center gap-1.5 px-3 py-1.5 rounded-xl bg-destructive/10 text-destructive hover:bg-destructive/20 transition-colors text-xs font-medium"
                title="Stop generating"
              >
                <StopCircle className="h-3.5 w-3.5" />
                <span>Stop</span>
              </button>
            ) : (
              <button
                onClick={handleSubmit}
                disabled={disabled}
                className="flex items-center justify-center h-8 w-8 rounded-xl bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
                title="Send message"
              >
                <ArrowUp className="h-4 w-4" />
              </button>
            )}
          </div>
        </div>
        {disabled && (
          <p className="mt-2 text-center text-[11px] text-muted-foreground/50">
            Create a new session from the sidebar to get started
          </p>
        )}
      </div>
    </div>
  )
}
