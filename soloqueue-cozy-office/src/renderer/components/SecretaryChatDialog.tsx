import { useState, useRef, useEffect } from 'react'
import { useSimStore } from '../store/simStore'
import { sounds } from '../utils/audio'

export default function SecretaryChatDialog({ onClose }: { onClose: () => void }) {
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
  }, [ fetchSessionStatus])

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
    <div className="absolute inset-0 z-30 flex items-center justify-center bg-[rgba(90,40,0,0.45)] backdrop-blur-sm animate-in fade-in duration-150">
      <div className="w-[400px] max-h-[520px] flex flex-col bg-[#f6ebd3] border-4 border-[#5a2800] shadow-[6px_6px_0px_#381a04] font-retro">
        {/* Title bar */}
        <div className="flex justify-between items-center bg-[#5f3e26] px-3 py-2 border-b-3 border-[#5a2800]">
          <div className="flex items-center gap-2">
            <span className="text-[16px]">👩‍💼</span>
            <span className="font-bold text-[#f6ebd3] text-[13px] tracking-wider">
              L1 CHIEF SECRETARY
            </span>
            <span className={`inline-block w-2.5 h-2.5 rounded-full ${sessionBusy ? 'bg-[#e28a2b] animate-pulse' : isConnected ? 'bg-[#4eb036]' : 'bg-[#8c7662]'}`} />
          </div>
          <button
            onClick={onClose}
            className="text-[#f6ebd3] hover:text-white text-[14px] font-bold px-1.5 border border-[#e6b053] hover:bg-[#e6b053]/20 rounded"
          >
            ✕
          </button>
        </div>

        {/* Status line */}
        <div className="px-3 py-1 bg-[#e3d3b4] border-b-2 border-[#5a2800] text-[9px] font-bold text-[#381a04]">
          {!isConnected
            ? '⚠ NOT CONNECTED — start backend first'
            : sessionBusy
              ? '⏳ PROCESSING...'
              : '✔ READY — type a message'}
        </div>

        {/* Messages */}
        <div ref={scrollRef} className="flex-1 overflow-y-auto px-3 py-2 space-y-2 min-h-[200px] max-h-[280px] bg-[#f6ebd3]">
          {sessionMessages.length === 0 ? (
            <div className="text-center py-8 text-[#8c7662] text-[11px] italic">
              Send a message to the L1 orchestrator.<br />
              It will route tasks to the appropriate teams.
            </div>
          ) : (
            sessionMessages.map((msg, i) => {
              const isUser = msg.role === 'user'
              return (
                <div key={i} className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
                  <div
                    className={`max-w-[85%] px-3 py-2 border-2 ${
                      isUser
                        ? 'bg-[#5f3e26] text-[#f6ebd3] border-[#381a04] rounded-t-lg rounded-bl-lg'
                        : 'bg-[#e3d3b4] text-[#381a04] border-[#b86a34] rounded-t-lg rounded-br-lg'
                    }`}
                  >
                    <div className="flex items-center gap-1.5 mb-0.5">
                      <span className="text-[9px] font-bold opacity-60">
                        {isUser ? 'YOU' : '👩‍💼 L1'}
                      </span>
                      {msg.timestamp && (
                        <span className="text-[7px] opacity-40 ml-auto">
                          {formatTime(msg.timestamp)}
                        </span>
                      )}
                    </div>
                    <p className="text-[11px] leading-relaxed whitespace-pre-wrap break-words">
                      {msg.content}
                    </p>
                  </div>
                </div>
              )
            })
          )}
          {sessionBusy && (
            <div className="flex justify-start">
              <div className="bg-[#e3d3b4] text-[#8c7662] border-2 border-[#b86a34] rounded-t-lg rounded-br-lg px-3 py-2 text-[10px] italic animate-pulse">
                Thinking...
              </div>
            </div>
          )}
        </div>

        {/* Input */}
        <div className="flex items-center gap-1.5 px-3 py-2 border-t-2 border-[#5a2800] bg-[#e3d3b4]">
          <input
            ref={inputRef}
            value={input}
            onChange={e => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={sessionBusy ? 'Waiting...' : 'Send a message...'}
            disabled={sessionBusy || !isConnected}
            className="flex-1 bg-[#f6ebd3] border-2 border-[#5a2800] px-2.5 py-1.5 text-[11px] text-[#381a04] placeholder-[#8c7662] font-retro outline-none disabled:opacity-50"
          />
          {sessionBusy ? (
            <button
              onClick={handleCancel}
              className="px-3 py-1.5 bg-[#d83838] text-[#f6ebd3] border-2 border-[#381a04] text-[10px] font-bold hover:brightness-110 active:translate-y-px"
            >
              ■ STOP
            </button>
          ) : (
            <>
              <button
                onClick={handleSend}
                disabled={!input.trim() || !isConnected}
                className="px-3 py-1.5 bg-[#4eb036] text-[#f6ebd3] border-2 border-[#381a04] text-[10px] font-bold hover:brightness-110 active:translate-y-px disabled:opacity-40"
              >
                ▶ SEND
              </button>
              <button
                onClick={handleClear}
                disabled={!isConnected}
                className="px-2 py-1.5 bg-[#e28a2b] text-[#f6ebd3] border-2 border-[#381a04] text-[10px] font-bold hover:brightness-110 active:translate-y-px disabled:opacity-40"
              >
                CLEAR
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
