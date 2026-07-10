import { type KeyboardEvent, useRef, useEffect, useCallback, useState, useMemo } from 'react'
import { toast } from 'sonner'
import { 
  ArrowUp, StopCircle, X, Loader2, Plus, ChevronDown, 
  Check, Laptop, GitBranch, Users, Cpu, Palette
} from 'lucide-react'
import { uploadFile, getProjectBranches, listFiles } from '@/lib/api'
import type { Project, FileInfo } from '@/types'
import { cn } from '@/lib/utils'
import { useRuntimeStore } from '@/stores/runtimeStore'

// ─── Autocomplete types ──────────────────────────────────────────────────────

interface AutocompleteItem {
  label: string        // displayed name (e.g. "l0", "my-skill")
  description: string  // short hint shown in the popup
  type: 'command' | 'skill'
}

// Built-in slash commands with descriptions
const BUILTIN_SLASH_COMMANDS: AutocompleteItem[] = [
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

export interface ChatInputProps {
  onSend: (
    text: string, 
    files?: { name: string; path: string }[],
    group?: string,
    projectPath?: string,
    selectedElement?: any
  ) => void
  onCancel: () => void
  streaming: boolean
  delegating: boolean
  disabled: boolean
  activeSessionId?: string
  processing?: boolean

  // Redesign selectors props
  showL2Selectors?: boolean
  groups?: string[]
  projects?: Project[]
  teamProjectsMap?: Record<string, Project[]>
  selectedGroup?: string
  selectedProjectPath?: string
  onGroupChange?: (group: string) => void
  onProjectChange?: (path: string) => void
  readOnlySelectors?: boolean
  ctxwinUsed?: number
  ctxwinLimit?: number

  // Agent status display (left of context ring)
  taskLevel?: string
  modelName?: string

  // Autocomplete: skill names fetched from /api/skills
  skillNames?: string[]

  // @ file completion: root directory (usually session project_path)
  atRootDir?: string

  // Selected preview element
  selectedTarget?: any
  onClearSelectedTarget?: () => void
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
  processing = false,
  showL2Selectors = false,
  groups = [],
  projects = [],
  teamProjectsMap = {},
  selectedGroup = '',
  selectedProjectPath = '',
  onGroupChange,
  onProjectChange,
  readOnlySelectors = false,
  ctxwinUsed = 0,
  ctxwinLimit = 0,
  taskLevel,
  modelName,
  skillNames = [],
  atRootDir = '',
  selectedTarget,
  onClearSelectedTarget,
}: ChatInputProps) {
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const groupRef = useRef<HTMLDivElement>(null)
  const projectRef = useRef<HTMLDivElement>(null)
  const branchRef = useRef<HTMLDivElement>(null)
  const [attachments, setAttachments] = useState<Attachment[]>([])
  const isDesignMode = useRuntimeStore((s) => s.isDesignMode)
  const setDesignMode = useRuntimeStore((s) => s.setDesignMode)
  const setSidebarCollapsed = useRuntimeStore((s) => s.setSidebarCollapsed)

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

  // ─── Inline highlight (backdrop-textarea) state ───────────────────────────
  const backdropInnerRef = useRef<HTMLDivElement>(null)
  const [inputValue, setInputValue] = useState('')
  // Maps display label (e.g. "src/ChatInput.tsx") → absolute path
  const [atMentions, setAtMentions] = useState<Map<string, string>>(new Map())

  // Selectors State
  const [activeDropdown, setActiveDropdown] = useState<'group' | 'project' | 'branch' | null>(null)
  const [dropdownPos, setDropdownPos] = useState<{ bottom: number; left: number } | null>(null)
  const [branch, setBranch] = useState<string>('main')
  const [branches, setBranches] = useState<string[]>(['main'])

  // Build autocomplete item list from current query
  const allAcItems = useMemo<AutocompleteItem[]>(() => {
    const skillItems: AutocompleteItem[] = skillNames.map((n) => ({
      label: n,
      description: 'Skill',
      type: 'skill',
    }))
    return [...BUILTIN_SLASH_COMMANDS, ...skillItems]
  }, [skillNames])

  // Sets for O(1) token-type lookup (used by backdrop highlight)
  const commandSet = useMemo(() => new Set(BUILTIN_SLASH_COMMANDS.map(c => c.label)), [])
  const skillSet   = useMemo(() => new Set(skillNames), [skillNames])

  // Build highlighted HTML for backdrop rendering
  const highlightedHtml = useMemo(() => {
    if (!inputValue) return ''
    // 1. HTML escape
    let result = inputValue
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')

    // 2. Extract @mention tokens FIRST (placeholder), so slash regex won't
    //    accidentally match path segments like /components in @src/components/
    const mentionMap: string[] = []
    result = result.replace(/(^|\s)(@\S+)/g, (_m, pre, mention) => {
      const idx = mentionMap.length
      mentionMap.push(mention) // includes @
      return `${pre}\x01${idx}\x01`
    })

    // 3. Highlight /command or /skill tokens (safe: @ paths already removed)
    result = result.replace(/(^|\s)(\/[a-z][a-z0-9-]*)/gi, (_m, pre, cmd) => {
      const word = cmd.slice(1).toLowerCase()
      if (commandSet.has(word)) {
        return `${pre}<span class="text-primary bg-primary/15">${cmd}</span>`
      } else if (skillSet.has(word)) {
        return `${pre}<span class="text-success bg-success/15">${cmd}</span>`
      }
      return `${pre}${cmd}`
    })

    // 4. Restore @mentions, highlighting resolved ones in sky-blue
    result = result.replace(/\x01(\d+)\x01/g, (_m, idxStr) => {
      const mention = mentionMap[Number(idxStr)] // includes @
      const label   = mention.slice(1)
      if (atMentions.has(label)) {
        return `<span class="text-sky-600 bg-sky-500/15 dark:text-sky-400">${mention}</span>`
      }
      return mention
    })

    // 5. Newlines → <br>, trailing nbsp preserves last-line height
    return result.replace(/\n/g, '<br>') + '\u00a0'
  }, [inputValue, commandSet, skillSet, atMentions])

  // Backdrop is needed when any token would be highlighted
  const hasHighlight = useMemo(() => {
    // Check for /command or /skill pattern
    if (/(^|\s)\/[a-z]/i.test(inputValue)) {
      const tokens = inputValue.match(/(?:^|\s)(\/[a-z][a-z0-9-]*)/gi) || []
      if (tokens.some(t => {
        const w = t.trim().slice(1).toLowerCase()
        return commandSet.has(w) || skillSet.has(w)
      })) return true
    }
    // Check for resolved @mentions
    return Array.from(atMentions.keys()).some(label => inputValue.includes(`@${label}`))
  }, [inputValue, commandSet, skillSet, atMentions])

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
    autoResize()
    setAcQuery(null)
    setInputValue(newVal)
    el.focus()
  }, [])

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
      setInputValue(newVal)
      autoResize()
      el.focus()
    } else {
      // File: resolve to display label + record absolute path mapping
      const lastSlash  = (atQuery ?? '').lastIndexOf('/')
      const currentDir = lastSlash >= 0 ? (atQuery ?? '').slice(0, lastSlash + 1) : ''
      const displayLabel = `${currentDir}${file.name}`
      const newVal       = `${before}@${displayLabel} ${after.trimStart()}`
      el.value = newVal
      el.setSelectionRange(atIdx + displayLabel.length + 2, atIdx + displayLabel.length + 2)
      setAtMentions(prev => new Map(prev).set(displayLabel, file.path))
      setAtQuery(null)
      setAtFiles([])
      setInputValue(newVal)
      autoResize()
      el.focus()
    }
  }, [atQuery])

  // Context window ring calculation
  const cwPct = useMemo(() => {
    if (!ctxwinLimit || ctxwinLimit <= 0) return 0
    return Math.min(100, Math.max(0, (ctxwinUsed / ctxwinLimit) * 100))
  }, [ctxwinUsed, ctxwinLimit])
  const cwRadius = 7
  const cwCircum = 2 * Math.PI * cwRadius
  const cwOffset = cwCircum - (cwPct / 100) * cwCircum

  // Compute fixed position for dropdown menus (must break out of overflow-x-auto clipping)
  useEffect(() => {
    if (activeDropdown) {
      const ref = activeDropdown === 'group' ? groupRef : activeDropdown === 'project' ? projectRef : branchRef
      const rect = ref.current?.getBoundingClientRect()
      if (rect) {
        setDropdownPos({
          bottom: window.innerHeight - rect.top + 4,
          left: rect.left,
        })
      }
    } else {
      setDropdownPos(null)
    }
  }, [activeDropdown])

  // Close dropdown when clicking outside
  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      const targets = [groupRef.current, projectRef.current, branchRef.current]
      const clickedInside = targets.some((ref) => ref && ref.contains(e.target as Node))
      if (!clickedInside) {
        setActiveDropdown(null)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  // Fetch branches dynamically based on the selected project path
  useEffect(() => {
    if (selectedProjectPath && projects.length > 0) {
      const proj = projects.find((p) => p.path === selectedProjectPath)
      if (proj) {
        getProjectBranches(proj.id)
          .then((list) => {
            setBranches(list)
            if (list.length > 0 && !list.includes(branch)) {
              setBranch(list[0])
            }
          })
          .catch(() => {
            setBranches(prev => (prev.length === 1 && prev[0] === 'main') ? prev : ['main'])
          })
      }
    } else {
      setBranches(prev => (prev.length === 1 && prev[0] === 'main') ? prev : ['main'])
    }
  }, [selectedProjectPath, projects])

  const filteredProjects = useMemo(() => {
    if (!selectedGroup || !teamProjectsMap) return []
    return teamProjectsMap[selectedGroup] || []
  }, [selectedGroup, teamProjectsMap])

  // Automatically select the first filtered project if the current selection is invalid
  useEffect(() => {
    if (showL2Selectors && selectedGroup && onProjectChange) {
      const activeProj = filteredProjects.find(p => p.path === selectedProjectPath)
      if (!activeProj && filteredProjects.length > 0) {
        onProjectChange(filteredProjects[0].path)
      } else if (filteredProjects.length === 0 && selectedProjectPath !== '') {
        onProjectChange('')
      }
    }
  }, [selectedGroup, filteredProjects, selectedProjectPath, onProjectChange, showL2Selectors])

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
            toast.error('Failed to upload image')
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
    const rawText = inputRef.current?.value.trim() || ''

    // Block sending if there are uploads in progress
    const hasUploading = attachments.some((att) => att.status === 'uploading')
    if (hasUploading) return

    const uploadedFiles = attachments
      .filter((att) => att.status === 'done' && att.path)
      .map((att) => ({ name: att.name, path: att.path! }))

    if ((!rawText && uploadedFiles.length === 0) || disabled) return

    // Replace @displayLabel → absolute path before sending to LLM
    let text = rawText
    atMentions.forEach((absPath, label) => {
      text = text.split(`@${label}`).join(absPath)
    })

    // Fallback prompt to satisfy backend non-empty check
    const finalPrompt =
      text ||
      (uploadedFiles.length === 1 ? `Pasted image: ${uploadedFiles[0].name}` : 'Pasted images')

    const selectedElement = selectedTarget ? {
      file_path: selectedTarget.filePath,
      selector: selectedTarget.selector,
      text: selectedTarget.text,
      html_hint: selectedTarget.htmlHint
    } : undefined

    onSend(
      finalPrompt,
      uploadedFiles.length > 0 ? uploadedFiles : undefined,
      selectedGroup || undefined,
      selectedProjectPath || undefined,
      selectedElement
    )

    if (inputRef.current) inputRef.current.value = ''
    setInputValue('')
    setAtMentions(new Map())

    // Clear and revoke attachments
    attachments.forEach((att) => URL.revokeObjectURL(att.previewUrl))
    setAttachments([])

    // Reset height
    if (inputRef.current) inputRef.current.style.height = 'auto'
  }, [disabled, onSend, attachments, selectedGroup, selectedProjectPath, atMentions])

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.nativeEvent.isComposing) return

    // @ file popup keyboard navigation (takes priority)
    if (atQuery !== null && (atFiles.length > 0 || atLoading)) {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setAtIndex(i => Math.min(i + 1, atFiles.length - 1))
        return
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        setAtIndex(i => Math.max(i - 1, 0))
        return
      }
      if (e.key === 'Enter' || e.key === 'Tab') {
        if (atFiles.length > 0) {
          e.preventDefault()
          applyAtMention(atFiles[atIndex])
          return
        }
      }
      if (e.key === 'Escape') {
        e.preventDefault()
        setAtQuery(null)
        setAtFiles([])
        return
      }
    }

    // Slash autocomplete keyboard navigation
    if (acQuery !== null && acItems.length > 0) {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setAcIndex((i) => Math.min(i + 1, acItems.length - 1))
        return
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        setAcIndex((i) => Math.max(i - 1, 0))
        return
      }
      if (e.key === 'Enter' || e.key === 'Tab') {
        e.preventDefault()
        applyAutocomplete(acItems[acIndex])
        return
      }
      if (e.key === 'Escape') {
        e.preventDefault()
        setAcQuery(null)
        return
      }
    }

    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSubmit()
    }
  }

  const autoResize = () => {
    const el = inputRef.current
    if (!el) return
    el.style.height = 'auto'
    const nextHeight = Math.min(el.scrollHeight, 160)
    el.style.height = nextHeight + 'px'
  }

  // Sync backdrop scroll with textarea scroll
  const handleScroll = useCallback(() => {
    if (backdropInnerRef.current && inputRef.current) {
      backdropInnerRef.current.style.marginTop = `-${inputRef.current.scrollTop}px`
    }
  }, [])

  // Update autocomplete query and inputValue on every input change
  const handleInput = useCallback(() => {
    autoResize()
    const el = inputRef.current
    if (!el) return
    const val = el.value
    setInputValue(val)

    const cursor  = el.selectionStart ?? val.length
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
  }, [atRootDir])


  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-2">
      {/* Input card */}
      <div className={cn(
        "relative flex flex-col rounded-xl border p-2.5 transition-all shadow-sm",
        processing 
          ? "border-primary/45 bg-primary/[0.015] shadow-inner" 
          : "border-border/40 bg-background focus-within:border-primary/30 focus-within:shadow-md"
      )}>
          {/* Thumbnails preview & Selected Element Badge */}
          {(attachments.length > 0 || selectedTarget) && (
            <div className="flex flex-wrap items-center gap-2 p-3 border-b border-border/40 bg-muted/5 rounded-t-xl">
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
                  {/* Hover action bar: preview / copy / download / remove */}
                  {att.status === 'done' && (
                    <div className="absolute inset-0 bg-black/0 group-hover/thumb:bg-black/50 transition-all flex flex-col items-center justify-center gap-1 opacity-0 group-hover/thumb:opacity-100">
                      {/* Open with system viewer */}
                      <button
                        title="Open with system viewer"
                        onClick={() => {
                          if (att.path) {
                            // Electron: open file with system default app
                            const api = (window as any).electronAPI
                            if (api?.openPath) {
                              api.openPath(att.path)
                            } else {
                              window.open(att.previewUrl, '_blank')
                            }
                          }
                        }}
                        className="h-5 w-5 rounded bg-white/20 hover:bg-white/40 flex items-center justify-center text-white transition-colors"
                      >
                        <svg viewBox="0 0 16 16" fill="currentColor" className="h-3 w-3">
                          <path d="M6.5 1A1.5 1.5 0 005 2.5V3H2.5A1.5 1.5 0 001 4.5v8A1.5 1.5 0 002.5 14h11A1.5 1.5 0 0015 12.5v-8A1.5 1.5 0 0013.5 3H11v-.5A1.5 1.5 0 009.5 1h-3zm0 1h3a.5.5 0 01.5.5V3H6v-.5a.5.5 0 01.5-.5zm6.5 2a.5.5 0 01.5.5v.634l-4.5 2.25-4.5-2.25V4.5a.5.5 0 01.5-.5h8z"/>
                        </svg>
                      </button>
                      {/* Copy to clipboard */}
                      <button
                        title="Copy image"
                        onClick={async () => {
                          try {
                            const res = await fetch(att.previewUrl)
                            const blob = await res.blob()
                            await navigator.clipboard.write([new ClipboardItem({ [blob.type]: blob })])
                          } catch {
                            // fallback: nothing
                          }
                        }}
                        className="h-5 w-5 rounded bg-white/20 hover:bg-white/40 flex items-center justify-center text-white transition-colors"
                      >
                        <svg viewBox="0 0 16 16" fill="currentColor" className="h-3 w-3">
                          <path d="M4 1.5H3a2 2 0 00-2 2V14a2 2 0 002 2h10a2 2 0 002-2V3.5a2 2 0 00-2-2h-1v1h1a1 1 0 011 1V14a1 1 0 01-1 1H3a1 1 0 01-1-1V3.5a1 1 0 011-1h1v-1z"/><path d="M9.5 1a.5.5 0 01.5.5v1a.5.5 0 01-.5.5h-3a.5.5 0 01-.5-.5v-1a.5.5 0 01.5-.5h3zm-3-1A1.5 1.5 0 005 1.5v1A1.5 1.5 0 006.5 4h3A1.5 1.5 0 0011 2.5v-1A1.5 1.5 0 009.5 0h-3z"/>
                        </svg>
                      </button>
                      {/* Download */}
                      <button
                        title="Download"
                        onClick={() => {
                          const a = document.createElement('a')
                          a.href = att.previewUrl
                          a.download = att.name
                          a.click()
                        }}
                        className="h-5 w-5 rounded bg-white/20 hover:bg-white/40 flex items-center justify-center text-white transition-colors"
                      >
                        <svg viewBox="0 0 16 16" fill="currentColor" className="h-3 w-3">
                          <path d="M.5 9.9a.5.5 0 01.5.5v2.1a1 1 0 001 1h12a1 1 0 001-1v-2.1a.5.5 0 011 0v2.1a2 2 0 01-2 2H2a2 2 0 01-2-2v-2.1a.5.5 0 01.5-.5z"/><path d="M7.646 11.854a.5.5 0 00.708 0l3-3a.5.5 0 00-.708-.708L8.5 10.293V1.5a.5.5 0 00-1 0v8.793L5.354 8.146a.5.5 0 10-.708.708l3 3z"/>
                        </svg>
                      </button>
                    </div>
                  )}
                  {/* Remove button (always visible on hover, top-right) */}
                  <button
                    onClick={() => removeAttachment(att.id)}
                    className="absolute top-1 right-1 h-4 w-4 rounded-full bg-black/60 hover:bg-black/80 flex items-center justify-center text-white opacity-0 group-hover/thumb:opacity-100 transition-opacity z-10"
                    title="Remove image"
                  >
                    <X className="h-2.5 w-2.5" />
                  </button>
                </div>
              ))}

              {selectedTarget && (
                <div className="flex items-center gap-1.5 px-2.5 py-1 rounded-lg border border-primary/25 bg-primary/5 text-primary text-[11px] font-medium animate-in fade-in slide-in-from-left-2 duration-200 max-w-full min-w-0">
                  <span className="font-semibold select-none flex-shrink-0">🌐 Selected DOM:</span>
                  <code className="bg-primary/10 px-1 py-0.5 rounded text-[10px] font-mono max-w-[180px] min-w-0 truncate" title={selectedTarget.selector}>
                    {selectedTarget.selector}
                  </code>
                  {selectedTarget.text && (
                    <span className="text-muted-foreground truncate max-w-[120px] min-w-0" title={selectedTarget.text}>
                      ("{selectedTarget.text}")
                    </span>
                  )}
                  <button
                    onClick={(e) => {
                      e.preventDefault();
                      onClearSelectedTarget?.();
                    }}
                    className="p-0.5 hover:bg-primary/15 rounded-full text-primary transition-colors cursor-pointer flex-shrink-0"
                    title="Deselect element"
                  >
                    <X className="h-2.5 w-2.5" />
                  </button>
                </div>
              )}
            </div>
          )}

          <div className="flex flex-col w-full min-h-[32px]">
            {/* Scoped CSS to create macOS-style inset overlay scrollbars */}
            <style>{`
              .hig-menu-scroll::-webkit-scrollbar {
                width: 8px;
                height: 8px;
              }
              .hig-menu-scroll::-webkit-scrollbar-track {
                background: transparent;
              }
              .hig-menu-scroll::-webkit-scrollbar-thumb {
                background-color: rgba(120, 120, 128, 0.25);
                border-radius: 9999px;
                border: 2px solid transparent;
                background-clip: padding-box;
              }
              .hig-menu-scroll::-webkit-scrollbar-thumb:hover {
                background-color: rgba(120, 120, 128, 0.45);
              }
            `}</style>

            {/* ── Textarea with backdrop highlight layer & absolute popups ── */}
            <div className="relative w-full">
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

              {/* Backdrop: renders highlighted tokens behind transparent textarea.
                  Font size and layout MUST exactly match the textarea to keep text aligned,
                  which both match the 13px (0.8125rem) / 1.6 font size of the chat area. */}
              {hasHighlight && (
                <div
                  aria-hidden="true"
                  className="absolute inset-0 pointer-events-none overflow-hidden select-none"
                  style={{
                    paddingTop: '6px',
                    paddingBottom: '6px',
                    paddingLeft: '12px',
                    paddingRight: '12px',
                    border: '1px solid transparent',
                    margin: '0',
                    fontSize: '13px',
                    lineHeight: '1.6',
                    fontFamily: 'ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, "Noto Sans", sans-serif',
                    fontWeight: '400',
                    letterSpacing: 'normal',
                    wordSpacing: 'normal',
                    color: 'var(--foreground)',
                  }}
                >
                  <div
                    ref={backdropInnerRef}
                    className="whitespace-pre-wrap break-words"
                    dangerouslySetInnerHTML={{ __html: highlightedHtml }}
                  />
                </div>
              )}
              <textarea
                ref={inputRef}
                className={cn(
                  'w-full resize-none bg-transparent focus:outline-none min-h-[32px] max-h-[160px] overflow-y-auto relative',
                  hasHighlight ? '' : 'text-foreground'
                )}
                style={{
                  paddingTop: '6px',
                  paddingBottom: '6px',
                  paddingLeft: '12px',
                  paddingRight: '12px',
                  border: '1px solid transparent',
                  margin: '0',
                  fontSize: '13px',
                  lineHeight: '1.6',
                  fontFamily: 'ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, "Noto Sans", sans-serif',
                  fontWeight: '400',
                  letterSpacing: 'normal',
                  wordSpacing: 'normal',
                  color: hasHighlight ? 'transparent' : 'var(--foreground)',
                  caretColor: 'var(--foreground)',
                  scrollbarWidth: 'none',
                }}
                placeholder={processing ? "Agent is working..." : "Ask anything..."}
                rows={1}
                disabled={disabled || processing}
                onKeyDown={handleKeyDown}
                onInput={handleInput}
                onScroll={handleScroll}
                onPaste={handlePaste}
              />
            </div>

            {/* Inner action buttons row */}
            <div className="flex items-center justify-between mt-2 pt-2 border-t border-border/15">
              {/* Left actions: plus and selectors */}
              <div className="flex items-center gap-2 flex-1 min-w-0">
                <button
                  type="button"
                  onClick={() => toast.info('Drag and drop or paste images to attach')}
                  className="p-1.5 rounded-lg hover:bg-muted text-muted-foreground/70 transition-colors cursor-pointer shrink-0"
                  title="Add context"
                >
                  <Plus className="h-3.5 w-3.5" />
                </button>
                {activeSessionId !== 'l1' && (
                <button
                  type="button"
                  onClick={() => {
                    const nextMode = !isDesignMode
                    setDesignMode(nextMode)
                    setSidebarCollapsed(nextMode)
                  }}
                  className={cn(
                    "p-1.5 rounded-lg transition-colors cursor-pointer shrink-0",
                    isDesignMode 
                      ? "bg-primary/20 text-primary border border-primary/20" 
                      : "hover:bg-muted text-muted-foreground/70"
                  )}
                  title="Design Mode"
                >
                  <Palette className="h-3.5 w-3.5" />
                </button>
                )}
                {showL2Selectors && (
                  <div className="flex items-center gap-1.5 text-xs text-muted-foreground select-none overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden py-0.5 flex-1 min-w-0">
                    {/* L2 Group Select */}
                    <div className="relative shrink-0" ref={groupRef}>
                      <button
                        type="button"
                        onClick={() => {
                          if (readOnlySelectors) return
                          setActiveDropdown(activeDropdown === 'group' ? null : 'group')
                        }}
                        className={cn(
                          "flex items-center gap-1 px-1.5 py-0.5 rounded-md text-[11px] font-semibold transition-colors text-muted-foreground/80",
                          readOnlySelectors 
                            ? "cursor-default" 
                            : "cursor-pointer hover:text-foreground hover:bg-muted/40"
                        )}
                      >
                        <Users className="h-2.5 w-2.5 text-muted-foreground/60" />
                        <span className="text-foreground/80">{selectedGroup || 'Select Group'}</span>
                        {!readOnlySelectors && <ChevronDown className="h-2.5 w-2.5 opacity-60" />}
                      </button>

                      {activeDropdown === 'group' && groups.length > 0 && (
                        <div
                          className="fixed z-50 w-44 rounded-xl border border-border bg-popover p-1 shadow-lg max-h-60 overflow-y-auto"
                          style={dropdownPos ? { bottom: `${dropdownPos.bottom}px`, left: `${dropdownPos.left}px` } : undefined}
                        >
                          {groups.map((g) => (
                            <button
                              key={g}
                              type="button"
                              onClick={() => {
                                if (onGroupChange) onGroupChange(g)
                                setActiveDropdown(null)
                              }}
                              className="flex w-full items-center justify-between px-2 py-1.5 text-left text-xs font-semibold rounded-lg hover:bg-muted text-foreground transition-colors"
                            >
                              <span>{g}</span>
                              {selectedGroup === g && <Check className="h-3 w-3 text-primary" />}
                            </button>
                          ))}
                        </div>
                      )}
                    </div>

                    {/* Project Select - Only show if the team actually has projects */}
                    {filteredProjects.length > 0 && (
                      <>
                        <span className="text-muted-foreground/20 font-mono select-none">/</span>
                        <div className="relative shrink-0" ref={projectRef}>
                          <button
                            type="button"
                            onClick={() => {
                              if (readOnlySelectors) return
                              setActiveDropdown(activeDropdown === 'project' ? null : 'project')
                            }}
                            className={cn(
                              "flex items-center gap-1 px-1.5 py-0.5 rounded-md text-[11px] font-semibold transition-colors text-muted-foreground/80",
                              readOnlySelectors 
                                ? "cursor-default" 
                                : "cursor-pointer hover:text-foreground hover:bg-muted/40"
                            )}
                          >
                            <Laptop className="h-2.5 w-2.5 text-muted-foreground/60" />
                            <span className="text-foreground/80 truncate max-w-[120px]">
                              {projects.find((p) => p.path === selectedProjectPath)?.name || 'Select Project'}
                            </span>
                            {!readOnlySelectors && <ChevronDown className="h-2.5 w-2.5 opacity-60" />}
                          </button>

                          {activeDropdown === 'project' && (
                            <div
                              className="fixed z-50 w-52 rounded-xl border border-border bg-popover p-1 shadow-lg max-h-60 overflow-y-auto"
                              style={dropdownPos ? { bottom: `${dropdownPos.bottom}px`, left: `${dropdownPos.left}px` } : undefined}
                            >
                              {filteredProjects.map((p) => (
                                <button
                                  key={p.id}
                                  type="button"
                                  onClick={() => {
                                    if (onProjectChange) onProjectChange(p.path)
                                    setActiveDropdown(null)
                                  }}
                                  className="flex w-full items-center justify-between px-2 py-1.5 text-left text-xs font-semibold rounded-lg hover:bg-muted text-foreground transition-colors"
                                >
                                  <span className="truncate">{p.name}</span>
                                  {selectedProjectPath === p.path && <Check className="h-3 w-3 text-primary" />}
                                </button>
                              ))}
                            </div>
                          )}
                        </div>
                      </>
                    )}

                    {/* Branch Select - Only show if projects are present and one is selected */}
                    {filteredProjects.length > 0 && selectedProjectPath && (
                      <>
                        <span className="text-muted-foreground/20 font-mono select-none">/</span>
                        <div className="relative shrink-0" ref={branchRef}>
                          <button
                            type="button"
                            onClick={() => {
                              if (readOnlySelectors) return
                              setActiveDropdown(activeDropdown === 'branch' ? null : 'branch')
                            }}
                            className={cn(
                              "flex items-center gap-1 px-1.5 py-0.5 rounded-md text-[11px] font-semibold transition-colors text-muted-foreground/80",
                              readOnlySelectors 
                                ? "cursor-default" 
                                : "cursor-pointer hover:text-foreground hover:bg-muted/40"
                            )}
                          >
                            <GitBranch className="h-2.5 w-2.5 text-muted-foreground/60" />
                            <span className="text-foreground/80">{branch}</span>
                            {!readOnlySelectors && <ChevronDown className="h-2.5 w-2.5 opacity-60" />}
                          </button>

                          {activeDropdown === 'branch' && (
                            <div
                              className="fixed z-50 w-32 rounded-xl border border-border bg-popover p-1 shadow-lg"
                              style={dropdownPos ? { bottom: `${dropdownPos.bottom}px`, left: `${dropdownPos.left}px` } : undefined}
                            >
                              {branches.map((b) => (
                                <button
                                  key={b}
                                  type="button"
                                  onClick={() => {
                                    setBranch(b)
                                    setActiveDropdown(null)
                                  }}
                                  className="flex w-full items-center justify-between px-2 py-1.5 text-left text-xs font-semibold rounded-lg hover:bg-muted text-foreground transition-colors"
                                >
                                  <span>{b}</span>
                                  {branch === b && <Check className="h-3 w-3 text-primary" />}
                                </button>
                              ))}
                            </div>
                          )}
                        </div>
                      </>
                    )}
                  </div>
                )}
              </div>

              {/* Right actions: task/model status, context window ring, send/stop */}
              <div className="flex items-center gap-2">
                {/* Task level + model badge (left of context ring) */}
                {(taskLevel || modelName || processing) && (
                  <div className="flex items-center gap-1.5 shrink-0 select-none">
                    {processing && (
                      <div className="flex items-center gap-1 text-primary mr-1 select-none">
                        <Loader2 className="h-3.5 w-3.5 animate-spin text-primary" />
                        <span className="text-[10px] font-bold font-mono tracking-wider animate-pulse">
                          THINKING
                        </span>
                      </div>
                    )}
                    {taskLevel && (
                      <span className="text-[10px] font-semibold text-primary bg-primary/10 border border-primary/20 px-1.5 py-0.5 rounded-md font-mono whitespace-nowrap">
                        {taskLevel.split('-')[0]}
                      </span>
                    )}
                    {modelName && (
                      <span
                        className="flex items-center gap-1 text-[10px] font-semibold text-muted-foreground/70 bg-muted/30 border border-border/20 px-1.5 py-0.5 rounded-md font-mono whitespace-nowrap max-w-[220px] truncate"
                        title={modelName}
                      >
                        <Cpu className="h-2.5 w-2.5 shrink-0 text-muted-foreground/50" />
                        <span className="truncate">{modelName}</span>
                      </span>
                    )}
                  </div>
                )}

                {ctxwinLimit > 0 && (
                  <div className="relative group/cw flex items-center">
                    <svg
                      width="20" height="20" viewBox="0 0 20 20"
                      className="-rotate-90 shrink-0"
                    >
                      {/* Background track */}
                      <circle
                        cx="10" cy="10" r={cwRadius}
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="1.5"
                        className="text-muted-foreground/15"
                      />
                      {/* Progress arc */}
                      <circle
                        cx="10" cy="10" r={cwRadius}
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="1.5"
                        strokeLinecap="round"
                        strokeDasharray={cwCircum}
                        strokeDashoffset={cwOffset}
                        className="text-primary transition-all duration-500 ease-out"
                      />
                    </svg>
                    {/* Hover tooltip */}
                    <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 hidden group-hover/cw:block z-50 pointer-events-none">
                      <div className="bg-popover border border-border rounded-xl px-3 py-2 shadow-xl whitespace-nowrap">
                        <p className="text-xs font-semibold text-foreground">
                          {cwPct.toFixed(1)}% used
                        </p>
                        <p className="text-[10px] text-muted-foreground mt-0.5">
                          {ctxwinUsed.toLocaleString()} / {ctxwinLimit.toLocaleString()} tokens
                        </p>
                      </div>
                    </div>
                  </div>
                )}

                {streaming || delegating || processing ? (
                  <button
                    type="button"
                    onClick={onCancel}
                    className="flex items-center gap-1 px-3 py-1 rounded-xl bg-destructive/10 text-destructive hover:bg-destructive/20 transition-all text-xs font-semibold cursor-pointer"
                  >
                    <StopCircle className="h-3.5 w-3.5" />
                    <span>Stop</span>
                  </button>
                ) : (
                  <button
                    type="button"
                    onClick={handleSubmit}
                    disabled={disabled || attachments.some((att) => att.status === 'uploading')}
                    className="flex items-center justify-center h-7 w-7 rounded-lg bg-secondary text-secondary-foreground hover:opacity-90 transition-all disabled:opacity-20 disabled:cursor-not-allowed cursor-pointer"
                  >
                    <ArrowUp className="h-3.5 w-3.5 stroke-[2.5]" />
                  </button>
                )}
              </div>
            </div>
          </div>
      </div>

      {disabled && !showL2Selectors && (
        <p className="mt-2 text-center text-[11px] text-muted-foreground/50">
          Create a new session from the sidebar to get started
        </p>
      )}
    </div>
  )
}
