import { useState, useRef, useEffect } from 'react'
import { useSimStore } from '../stores/simStore'
import { sounds } from '../utils/audio'

export default function SecretaryChatDialog() {
  const isConnected = useSimStore(s => s.isConnected)
  const sessionMessages = useSimStore(s => s.sessionMessages)
  const sessionBusy = useSimStore(s => s.sessionBusy)
  const sendSessionPrompt = useSimStore(s => s.sendSessionPrompt)
  const cancelSessionTask = useSimStore(s => s.cancelSessionTask)
  const clearSessionHistory = useSimStore(s => s.clearSessionHistory)
  const fetchSessionStatus = useSimStore(s => s.fetchSessionStatus)

  const [input, setInput] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // Load session status on mount
  useEffect(() => {
    if (isConnected) {
      fetchSessionStatus()
    }
  }, [fetchSessionStatus])

  // Auto-scroll to bottom
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [sessionMessages])

  // Start polling while busy
  useEffect(() => {
    if (!sessionBusy) return
    const interval = setInterval(() => {
      fetchSessionStatus()
    }, 1500)
    return () => clearInterval(interval)
  }, [sessionBusy, fetchSessionStatus])

  const handleSend = async () => {
    const text = input.trim()
    if (!text || sessionBusy) return
    setInput('')
    sounds.playSelect()
    await sendSessionPrompt(text)
    fetchSessionStatus()
  }

  const handleCancel = () => {
    sounds.playSelect()
    cancelSessionTask()
  }

  const handleClear = () => {
    sounds.playSelect()
    clearSessionHistory()
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  const formatTime = (ts: string) => {
    try {
      return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    } catch {
      return ''
    }
  }

  return (
    <div className="flex flex-col h-full bg-[#1a0f08] font-retro overflow-hidden">
      {/* Status line */}
      <div className="px-3 py-1.5 bg-[#241a0e] border-b border-[#e6b053]/20 text-[10px] font-bold text-[#f6ebd3]">
        {!isConnected
          ? '⚠ NOT CONNECTED — start backend first'
          : sessionBusy
            ? '⏳ PROCESSING...'
            : '✔ READY — type a message'}
      </div>

      {/* Messages */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto px-3 py-3 space-y-3 bg-[#1a0f08]">
        {sessionMessages.length === 0 ? (
          <div className="text-center py-12 text-[#8c7662] text-[12px] italic leading-normal">
            Send a message to the L1 orchestrator.<br />
            It will route tasks to the appropriate teams.
          </div>
        ) : (
          sessionMessages.map((msg, i) => {
            const isUser = msg.role === 'user'
            return (
              <div key={i} className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
                <div
                  className={`max-w-[90%] px-3 py-2 border rounded-lg ${
                    isUser
                      ? 'bg-[#e6b053]/15 text-[#f6ebd3] border-[#e6b053]/30 rounded-tr-none'
                      : 'bg-[#241a0e] text-[#f6ebd3] border-[#e6b053]/15 rounded-tl-none'
                  }`}
                >
                  <div className="flex items-center gap-2 mb-1">
                    <span className="text-[9px] font-bold text-[#e6b053] opacity-80">
                      {isUser ? 'YOU' : '👩‍💼 L1'}
                    </span>
                    {msg.timestamp && (
                      <span className="text-[8px] text-[#8c7662] ml-auto">
                        {formatTime(msg.timestamp)}
                      </span>
                    )}
                  </div>
                  <p className="text-[12px] leading-relaxed whitespace-pre-wrap break-words">
                    {msg.content}
                  </p>
                </div>
              </div>
            )
          })
        )}
        {sessionBusy && (
          <div className="flex justify-start">
            <div className="bg-[#241a0e] text-[#8c7662] border border-[#e6b053]/15 rounded-lg rounded-tl-none px-3 py-2 text-[11px] italic animate-pulse">
              Thinking...
            </div>
          </div>
        )}
      </div>

      {/* Input */}
      <div className="flex flex-col gap-2 p-3 border-t border-[#e6b053]/20 bg-[#241a0e]/40">
        <div className="flex gap-2">
          <input
            ref={inputRef}
            value={input}
            onChange={e => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={sessionBusy ? 'Waiting...' : 'Send a message...'}
            disabled={sessionBusy || !isConnected}
            className="flex-1 bg-[#1a0f08] border border-[#e6b053]/30 rounded px-2.5 py-1.5 text-[11px] text-[#f6ebd3] placeholder-[#8c7662] font-retro outline-none disabled:opacity-50 focus:border-[#e6b053] transition-colors"
          />
        </div>
        <div className="flex gap-2 justify-end">
          {sessionBusy ? (
            <button
              onClick={handleCancel}
              className="px-3 py-1.5 bg-[#d83838] text-white border border-red-700 rounded text-[10px] font-bold hover:brightness-110 active:translate-y-px transition-all"
            >
              ■ STOP
            </button>
          ) : (
            <>
              <button
                onClick={handleClear}
                disabled={!isConnected}
                className="px-3 py-1.5 bg-[#e28a2b]/20 text-[#e28a2b] border border-[#e28a2b]/40 rounded text-[10px] font-bold hover:bg-[#e28a2b]/30 active:translate-y-px disabled:opacity-40 transition-all cursor-pointer"
              >
                CLEAR
              </button>
              <button
                onClick={handleSend}
                disabled={!input.trim() || !isConnected}
                className="px-4 py-1.5 bg-[#e6b053] text-[#1a0f08] rounded text-[10px] font-bold hover:bg-[#f6ebd3] active:translate-y-px disabled:opacity-40 transition-all cursor-pointer"
              >
                ▶ SEND
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
