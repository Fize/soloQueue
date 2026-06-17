import { type KeyboardEvent, useRef, useEffect, useCallback, useState } from 'react'
import { ArrowUp, StopCircle, X, Loader2 } from 'lucide-react'
import { uploadFile } from '@/lib/api'

export interface ChatInputProps {
  onSend: (text: string, files?: { name: string; path: string }[]) => void
  onCancel: () => void
  streaming: boolean
  delegating: boolean
  disabled: boolean
  activeSessionId?: string
}

interface Attachment {
  id: string
  file: File
  name: string
  previewUrl: string
  status: 'uploading' | 'done' | 'failed'
  path?: string
  error?: string
}

export function ChatInput({
  onSend,
  onCancel,
  streaming,
  delegating,
  disabled,
  activeSessionId,
}: ChatInputProps) {
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const [attachments, setAttachments] = useState<Attachment[]>([])

  useEffect(() => {
    if (!streaming) {
      inputRef.current?.focus()
    }
  }, [streaming])

  // Cleanup object URLs on unmount
  const attachmentsRef = useRef<Attachment[]>([])
  useEffect(() => {
    attachmentsRef.current = attachments
  }, [attachments])

  useEffect(() => {
    return () => {
      attachmentsRef.current.forEach((att) => {
        URL.revokeObjectURL(att.previewUrl)
      })
    }
  }, [])

  const handlePaste = useCallback(
    async (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
      const items = e.clipboardData.items
      const filesToUpload: File[] = []
      for (let i = 0; i < items.length; i++) {
        const item = items[i]
        if (item.type.indexOf('image') !== -1) {
          const file = item.getAsFile()
          if (file) {
            filesToUpload.push(file)
          }
        }
      }

      if (filesToUpload.length === 0) return

      // Prevent pasting raw image content into textarea text
      e.preventDefault()

      for (const file of filesToUpload) {
        const id = Math.random().toString(36).substring(2, 9)
        const previewUrl = URL.createObjectURL(file)

        const newAttachment: Attachment = {
          id,
          file,
          name: file.name || `image-${Date.now()}.png`,
          previewUrl,
          status: 'uploading',
        }

        setAttachments((prev) => [...prev, newAttachment])

        uploadFile(file, activeSessionId)
          .then((res) => {
            setAttachments((prev) =>
              prev.map((att) => (att.id === id ? { ...att, status: 'done', path: res.path } : att))
            )
          })
          .catch((err) => {
            console.error('Failed to upload pasted image:', err)
            setAttachments((prev) =>
              prev.map((att) =>
                att.id === id
                  ? { ...att, status: 'failed', error: err.message || 'Upload failed' }
                  : att
              )
            )
          })
      }
    },
    [activeSessionId]
  )

  const removeAttachment = useCallback((id: string) => {
    setAttachments((prev) => {
      const target = prev.find((att) => att.id === id)
      if (target) {
        URL.revokeObjectURL(target.previewUrl)
      }
      return prev.filter((att) => att.id !== id)
    })
  }, [])

  const handleSubmit = useCallback(() => {
    const text = inputRef.current?.value.trim() || ''

    // Block sending if there are uploads in progress
    const hasUploading = attachments.some((att) => att.status === 'uploading')
    if (hasUploading) return

    const uploadedFiles = attachments
      .filter((att) => att.status === 'done' && att.path)
      .map((att) => ({ name: att.name, path: att.path! }))

    if ((!text && uploadedFiles.length === 0) || streaming || disabled) return

    // Fallback prompt to satisfy backend non-empty check
    const finalPrompt =
      text ||
      (uploadedFiles.length === 1 ? `Pasted image: ${uploadedFiles[0].name}` : 'Pasted images')

    onSend(finalPrompt, uploadedFiles.length > 0 ? uploadedFiles : undefined)

    if (inputRef.current) inputRef.current.value = ''

    // Clear and revoke attachments
    attachments.forEach((att) => URL.revokeObjectURL(att.previewUrl))
    setAttachments([])

    // Reset height
    if (inputRef.current) inputRef.current.style.height = 'auto'
  }, [streaming, disabled, onSend, attachments])

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.nativeEvent.isComposing) return
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSubmit()
    }
  }

  const autoResize = () => {
    const el = inputRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = el.scrollHeight + 'px'
  }

  const placeholderText = disabled
    ? 'Select a session to begin...'
    : 'Ask anything, or paste an image — Enter to send, Shift+Enter for newline'

  return (
    <div className="border-t border-border/50 bg-gradient-to-t from-card to-card/80 backdrop-blur-sm">
      <div className="mx-auto max-w-3xl px-4 py-4">
        <div className="relative flex flex-col rounded-2xl border border-border/60 bg-background shadow-sm transition-shadow focus-within:shadow-md focus-within:border-primary/30 focus-within:ring-2 focus-within:ring-primary/5">
          {/* Thumbnails preview */}
          {attachments.length > 0 && (
            <div className="flex flex-wrap gap-2 p-3 border-b border-border/40 bg-muted/5 rounded-t-2xl">
              {attachments.map((att) => (
                <div
                  key={att.id}
                  className="relative group/thumb h-16 w-16 rounded-lg overflow-hidden border border-border bg-muted/30"
                >
                  <img src={att.previewUrl} alt="preview" className="h-full w-full object-cover" />
                  {att.status === 'uploading' && (
                    <div className="absolute inset-0 bg-black/40 flex items-center justify-center">
                      <Loader2 className="h-4 w-4 animate-spin text-white" />
                    </div>
                  )}
                  {att.status === 'failed' && (
                    <div
                      className="absolute inset-0 bg-destructive/80 flex items-center justify-center"
                      title={att.error}
                    >
                      <span className="text-[10px] text-white font-medium">Failed</span>
                    </div>
                  )}
                  <button
                    onClick={() => removeAttachment(att.id)}
                    className="absolute top-1 right-1 h-4 w-4 rounded-full bg-black/60 hover:bg-black/80 flex items-center justify-center text-white opacity-0 group-hover/thumb:opacity-100 transition-opacity"
                    title="Remove image"
                  >
                    <X className="h-2.5 w-2.5" />
                  </button>
                </div>
              ))}
            </div>
          )}

          <div className="flex items-end w-full">
            <textarea
              ref={inputRef}
              className="flex-1 resize-none bg-transparent px-4 py-3 text-[15px] leading-relaxed text-foreground placeholder:text-muted-foreground/50 focus:outline-none min-h-[48px] rounded-2xl"
              placeholder={placeholderText}
              rows={1}
              disabled={streaming || disabled}
              onKeyDown={handleKeyDown}
              onInput={autoResize}
              onPaste={handlePaste}
            />
            <div className="shrink-0 flex items-center gap-1 pr-2 pb-2">
              {streaming && !delegating ? (
                <button
                  onClick={onCancel}
                  className="flex items-center gap-1.5 px-3 py-1.5 rounded-xl bg-destructive/10 text-destructive hover:bg-destructive/20 transition-colors text-xs font-medium"
                  title="Stop generating"
                >
                  <StopCircle className="h-3.5 w-3.5" />
                  <span>Stop</span>
                </button>
              ) : delegating ? (
                <div className="flex items-center gap-1.5 px-3 py-1.5 rounded-xl bg-violet-500/10 text-violet-500 text-xs font-medium">
                  <span className="inline-block h-2 w-2 rounded-full bg-violet-500 animate-pulse" />
                  <span>Delegating</span>
                </div>
              ) : (
                <button
                  onClick={handleSubmit}
                  disabled={disabled || attachments.some((att) => att.status === 'uploading')}
                  className="flex items-center justify-center h-8 w-8 rounded-xl bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
                  title="Send message"
                >
                  <ArrowUp className="h-4 w-4" />
                </button>
              )}
            </div>
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
