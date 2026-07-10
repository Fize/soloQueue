import { type KeyboardEvent, useRef, useEffect, useCallback, useState, useMemo, forwardRef, useImperativeHandle } from 'react'
import { Loader2 } from 'lucide-react'
import { listFiles } from '@/lib/api'
import type { FileInfo } from '@/types'
import { cn } from '@/lib/utils'

// ─── Autocomplete types ──────────────────────────────────────────────────────

export interface AutocompleteItem {
  label: string        // displayed name (e.g. "l0", "my-skill")
  description: string  // short hint shown in the popup
  type: 'command' | 'skill'
}

// Built-in slash commands with descriptions
export const BUILTIN_SLASH_COMMANDS: AutocompleteItem[] = [
  // ── Session control ──────────────────────────────────────────────────────
  { label: 'compact',   description: 'Compact context window (no memory save)', type: 'command' },
  { label: 'clear',     description: 'Clear dialogue history',                 type: 'command' },
  { label: 'cancel',    description: 'Cancel current task',                    type: 'command' },
  { label: 'help',      description: 'View available commands',                type: 'command' },
  { label: 'version',   description: 'View version number',                    type: 'command' },
  { label: 'cron',      description: 'Create scheduled task (cron expression)', type: 'command' },
  // ── Routing level locks ──────────────────────────────────────────────────
  { label: 'l0',        description: 'Force conversation level (no tools)',    type: 'command' },
  { label: 'l1',        description: 'Force simple single-file task level',    type: 'command' },
  { label: 'l2',        description: 'Force multi-file task level',            type: 'command' },
  { label: 'l3',        description: 'Force expert / complex level',           type: 'command' },
]

// ─── Component types ─────────────────────────────────────────────────────────

export interface ChatInputAutocompleteHandle {
  handleInput: () => void
  handleKeyDown: (e: KeyboardEvent<HTMLTextAreaElement>) => boolean
}

export interface ChatInputAutocompleteProps {
  value: string
  inputRef: React.RefObject<HTMLTextAreaElement>
  skillNames: string[]
  atRootDir: string
  onSelectMention: (path: string, name: string) => void
  onValueChange: () => void
}

// ─── Component ───────────────────────────────────────────────────────────────

export const ChatInputAutocomplete = forwardRef<ChatInputAutocompleteHandle, ChatInputAutocompleteProps>(
  function ChatInputAutocomplete({ value: _value, inputRef, skillNames, atRootDir, onSelectMention, onValueChange }, ref) {
    // ─── Slash autocomplete state ────────────────────────────────────────────
    const autocompleteRef = useRef<HTMLDivElement>(null)
    const [acQuery, setAcQuery] = useState<string | null>(null)   // null = hidden
    const [acIndex, setAcIndex] = useState(0)

    // ─── @ file completion state ──────────────────────────────────────────────
    const atRef = useRef<HTMLDivElement>(null)
    const [atQuery, setAtQuery] = useState<string | null>(null)   // null = hidden; string = partial path
    const [atFiles, setAtFiles] = useState<FileInfo[]>([])
    const [atIndex, setAtIndex] = useState(0)
    const [atLoading, setAtLoading] = useState(false)

    // Build autocomplete item list from current query
    const allAcItems = useMemo<AutocompleteItem[]>(() => {
      const skillItems: AutocompleteItem[] = skillNames.map((n) => ({
        label: n,
        description: 'Skill',
        type: 'skill',
      }))
      return [...BUILTIN_SLASH_COMMANDS, ...skillItems]
    }, [skillNames])

    const acItems = useMemo<AutocompleteItem[]>(() => {
      if (acQuery === null) return []
      const q = acQuery.toLowerCase()
      return allAcItems.filter((item) => item.label.toLowerCase().startsWith(q))
    }, [acQuery, allAcItems])

    // Reset selected index when list changes
    useEffect(() => {
      setAcIndex(0)
    }, [acItems.length])

    // Scroll selected slash-autocomplete item into view
    useEffect(() => {
      if (!autocompleteRef.current) return
      const el = autocompleteRef.current.querySelector<HTMLElement>(`[data-ac-idx="${acIndex}"]`)
      el?.scrollIntoView({ block: 'nearest' })
    }, [acIndex])

    // Scroll selected @ file item into view
    useEffect(() => {
      if (!atRef.current) return
      const el = atRef.current.querySelector<HTMLElement>(`[data-at-idx="${atIndex}"]`)
      el?.scrollIntoView({ block: 'nearest' })
    }, [atIndex])

    // Close slash autocomplete when clicking outside
    useEffect(() => {
      function handleOutside(e: MouseEvent) {
        if (
          autocompleteRef.current &&
          !autocompleteRef.current.contains(e.target as Node) &&
          inputRef.current &&
          !inputRef.current.contains(e.target as Node)
        ) {
          setAcQuery(null)
        }
      }
      document.addEventListener('mousedown', handleOutside)
      return () => document.removeEventListener('mousedown', handleOutside)
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [])

    // Close @ file popup when clicking outside
    useEffect(() => {
      function handleAtOutside(e: MouseEvent) {
        if (
          atRef.current &&
          !atRef.current.contains(e.target as Node) &&
          inputRef.current &&
          !inputRef.current.contains(e.target as Node)
        ) {
          setAtQuery(null)
          setAtFiles([])
        }
      }
      document.addEventListener('mousedown', handleAtOutside)
      return () => document.removeEventListener('mousedown', handleAtOutside)
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [])

    // Fetch files when @ query changes
    useEffect(() => {
      if (atQuery === null || !atRootDir) {
        setAtFiles([])
        return
      }
      const lastSlash = atQuery.lastIndexOf('/')
      const subDir   = lastSlash >= 0 ? atQuery.slice(0, lastSlash + 1) : ''
      const prefix   = lastSlash >= 0 ? atQuery.slice(lastSlash + 1) : atQuery
      const searchDir = subDir ? `${atRootDir}/${subDir}` : atRootDir

      let cancelled = false
      setAtLoading(true)
      listFiles(searchDir)
        .then(files => {
          if (cancelled) return
          const filtered = files.filter(f =>
            f.name.toLowerCase().startsWith(prefix.toLowerCase())
          )
          setAtFiles(filtered)
          setAtIndex(0)
        })
        .catch(() => { if (!cancelled) setAtFiles([]) })
        .finally(() => { if (!cancelled) setAtLoading(false) })
      return () => { cancelled = true }
    }, [atQuery, atRootDir])

    // Apply selected autocomplete item into textarea
    const applyAutocomplete = useCallback((item: AutocompleteItem) => {
      const el = inputRef.current
      if (!el) return
      const val = el.value
      const cursor = el.selectionStart ?? val.length
      // Find the start of the current /word token
      const slashIdx = val.lastIndexOf('/', cursor)
      if (slashIdx === -1) return
      const before = val.slice(0, slashIdx)
      const after = val.slice(cursor)
      const newVal = `${before}/${item.label} ${after.trimStart()}`
      el.value = newVal
      const newCursor = slashIdx + item.label.length + 2  // after the space
      el.setSelectionRange(newCursor, newCursor)
      setAcQuery(null)
      onValueChange()
      el.focus()
    }, [inputRef, onValueChange])

    // Apply selected @ file mention into textarea
    const applyAtMention = useCallback((file: FileInfo) => {
      const el = inputRef.current
      if (!el) return
      const val = el.value
      const cursor = el.selectionStart ?? val.length
      // Find the @ token before cursor
      const segment = val.slice(0, cursor)
      const atIdx   = segment.lastIndexOf('@')
      if (atIdx === -1) return
      const before = val.slice(0, atIdx)
      const after  = val.slice(cursor)

      if (file.isDir) {
        // Directory: continue navigating, update query
        const lastSlash = (atQuery ?? '').lastIndexOf('/')
        const currentDir = lastSlash >= 0 ? (atQuery ?? '').slice(0, lastSlash + 1) : ''
        const newQuery = `${currentDir}${file.name}/`
        const newVal   = `${before}@${newQuery}${after.trimStart()}`
        el.value = newVal
        el.setSelectionRange(atIdx + newQuery.length + 1, atIdx + newQuery.length + 1)
        setAtQuery(newQuery)
        onValueChange()
        el.focus()
      } else {
        // File: resolve to display label + record absolute path mapping
        const lastSlash  = (atQuery ?? '').lastIndexOf('/')
        const currentDir = lastSlash >= 0 ? (atQuery ?? '').slice(0, lastSlash + 1) : ''
        const displayLabel = `${currentDir}${file.name}`
        const newVal       = `${before}@${displayLabel} ${after.trimStart()}`
        el.value = newVal
        el.setSelectionRange(atIdx + displayLabel.length + 2, atIdx + displayLabel.length + 2)
        onSelectMention(file.path, displayLabel)
        setAtQuery(null)
        setAtFiles([])
        onValueChange()
        el.focus()
      }
    }, [atQuery, inputRef, onSelectMention, onValueChange])

    // ─── Exposed handlers via ref ──────────────────────────────────────────

    const handleInput = useCallback(() => {
      const el = inputRef.current
      if (!el) return
      const val = el.value
      const cursor = el.selectionStart ?? val.length
      const segment = val.slice(0, cursor)

      // Detect @ file token (takes priority over /)
      const atMatch = segment.match(/(^|\s)@([^\s]*)$/)
      if (atMatch && atRootDir) {
        setAtQuery(atMatch[2])
        setAcQuery(null)
        return
      }
      setAtQuery(null)
      setAtFiles([])

      // Detect / slash token
      const slashMatch = segment.match(/(?:^|\s)\/([a-z0-9-]*)$/i)
      if (slashMatch) {
        setAcQuery(slashMatch[1])
      } else {
        setAcQuery(null)
      }
    }, [atRootDir, inputRef])

    const handleKeyDown = useCallback((e: KeyboardEvent<HTMLTextAreaElement>): boolean => {
      // @ file popup keyboard navigation (takes priority)
      if (atQuery !== null && (atFiles.length > 0 || atLoading)) {
        if (e.key === 'ArrowDown') {
          e.preventDefault()
          setAtIndex(i => Math.min(i + 1, atFiles.length - 1))
          return true
        }
        if (e.key === 'ArrowUp') {
          e.preventDefault()
          setAtIndex(i => Math.max(i - 1, 0))
          return true
        }
        if (e.key === 'Enter' || e.key === 'Tab') {
          if (atFiles.length > 0) {
            e.preventDefault()
            applyAtMention(atFiles[atIndex])
            return true
          }
          if (atLoading) {
            e.preventDefault()
            return true
          }
        }
        if (e.key === 'Escape') {
          e.preventDefault()
          setAtQuery(null)
          setAtFiles([])
          return true
        }
      }

      // Slash autocomplete keyboard navigation
      if (acQuery !== null && acItems.length > 0) {
        if (e.key === 'ArrowDown') {
          e.preventDefault()
          setAcIndex((i) => Math.min(i + 1, acItems.length - 1))
          return true
        }
        if (e.key === 'ArrowUp') {
          e.preventDefault()
          setAcIndex((i) => Math.max(i - 1, 0))
          return true
        }
        if (e.key === 'Enter' || e.key === 'Tab') {
          e.preventDefault()
          applyAutocomplete(acItems[acIndex])
          return true
        }
        if (e.key === 'Escape') {
          e.preventDefault()
          setAcQuery(null)
          return true
        }
      }

      return false
    }, [atQuery, atFiles, atLoading, applyAtMention, acQuery, acItems, acIndex, applyAutocomplete])

    useImperativeHandle(ref, () => ({
      handleInput,
      handleKeyDown,
    }), [handleInput, handleKeyDown])

    return (
      <>
        {/* ── / slash command autocomplete popup ─────────────────────── */}
        {acItems.length > 0 && acQuery !== null && (
          <div
            ref={autocompleteRef}
            className="absolute bottom-full left-0 right-0 w-full z-50 rounded-[13px] border border-border/20 bg-background/80 backdrop-blur-[20px] saturate-[1.9] shadow-[0_4px_30px_rgba(0,0,0,0.03),0_1px_3px_rgba(0,0,0,0.02)] overflow-hidden p-1 mb-2 animate-in fade-in slide-in-from-bottom-1 zoom-in-95 duration-150 ease-out"
          >
            {/* Scrollable area inside outer container to prevent scrollbar corner overflow */}
            <div
              className="overflow-y-auto hig-menu-scroll pr-0.5"
              style={{
                maxHeight: '210px',
              }}
            >
              <div className="flex flex-col gap-0.5">
                {acItems.map((item, idx) => (
                  <button
                    key={item.label}
                    type="button"
                    data-ac-idx={idx}
                    onMouseDown={(e) => {
                      e.preventDefault()
                      applyAutocomplete(item)
                    }}
                    onMouseEnter={() => setAcIndex(idx)}
                    className={cn(
                      'w-full flex items-center gap-3 px-3 py-1.5 min-h-[40px] rounded-lg text-left transition-all duration-150 ease-out',
                      idx === acIndex 
                        ? 'bg-primary/10 text-primary' 
                        : 'text-foreground hover:bg-primary/5 hover:text-primary'
                    )}
                  >
                    <span className={cn(
                      'text-[10px] font-mono font-semibold px-1.5 py-0.5 rounded shrink-0',
                      item.type === 'command'
                        ? 'bg-primary/15 text-primary'
                        : 'bg-success/15 text-success'
                    )}>
                      /{item.label}
                    </span>
                    <span className="text-[12px] text-muted-foreground truncate">{item.description}</span>
                  </button>
                ))}
              </div>
            </div>
          </div>
        )}

        {/* ── @ file autocomplete popup ──────────────────────────────── */}
        {atQuery !== null && (atFiles.length > 0 || atLoading) && (
          <div
            ref={atRef}
            className="absolute bottom-full left-0 right-0 w-full z-50 rounded-[13px] border border-border/20 bg-background/80 backdrop-blur-[20px] saturate-[1.9] shadow-[0_4px_30px_rgba(0,0,0,0.03),0_1px_3px_rgba(0,0,0,0.02)] overflow-hidden p-1 mb-2 animate-in fade-in slide-in-from-bottom-1 zoom-in-95 duration-150 ease-out"
          >
            {/* Inner scroll container keeps scrollbar away from outer rounded border */}
            <div
              className="overflow-y-auto hig-menu-scroll pr-0.5"
              style={{
                maxHeight: '210px',
              }}
            >
              <div className="flex flex-col gap-0.5">
                {atLoading && atFiles.length === 0 && (
                  <div className="flex items-center gap-2 px-3 py-2 text-xs text-muted-foreground">
                    <Loader2 className="h-3 w-3 animate-spin" />
                    <span>Loading…</span>
                  </div>
                )}
                {atFiles.map((file, idx) => (
                  <button
                    key={file.path}
                    type="button"
                    data-at-idx={idx}
                    onMouseDown={(e) => {
                      e.preventDefault()
                      applyAtMention(file)
                    }}
                    onMouseEnter={() => setAtIndex(idx)}
                    className={cn(
                      'w-full flex items-center gap-2.5 px-3 py-1.5 min-h-[40px] rounded-lg text-left transition-all duration-150 ease-out',
                      idx === atIndex 
                        ? 'bg-primary/10 text-primary' 
                        : 'text-foreground hover:bg-primary/5 hover:text-primary'
                    )}
                  >
                    {/* Icon */}
                    <span className="text-[12px] shrink-0 leading-none">
                      {file.isDir ? '📁' : '📄'}
                    </span>
                    {/* Name */}
                    <span className="text-[13px] font-medium truncate">{file.name}</span>
                    {/* Dir indicator */}
                    {file.isDir && (
                      <span className="ml-auto text-[11px] text-muted-foreground/50 shrink-0">/</span>
                    )}
                    {/* File size hint */}
                    {!file.isDir && file.size > 0 && (
                      <span className="ml-auto text-[10px] text-muted-foreground/40 shrink-0 font-mono">
                        {file.size < 1024
                          ? `${file.size}B`
                          : file.size < 1024 * 1024
                          ? `${(file.size / 1024).toFixed(1)}K`
                          : `${(file.size / 1024 / 1024).toFixed(1)}M`}
                      </span>
                    )}
                  </button>
                ))}
              </div>
            </div>
          </div>
        )}
      </>
    )
  }
)
