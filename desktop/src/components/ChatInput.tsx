import { useTranslation } from '@/lib/i18n'
import { type KeyboardEvent, useRef, useEffect, useLayoutEffect, useCallback, useState, useMemo } from 'react'
import { createPortal } from 'react-dom'
import { toast } from 'sonner'
import {
  ArrowUp, StopCircle, Plus, ChevronDown,
  Check, Laptop, GitBranch, Users, Palette
} from 'lucide-react'
import { uploadFile, getProjectBranches } from '@/lib/api'
import type { Project } from '@/types'
import { cn } from '@/lib/utils'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { ChatInputAutocomplete, BUILTIN_SLASH_COMMANDS, type ChatInputAutocompleteHandle } from './ChatInputAutocomplete'
import { ChatInputAttachments, type Attachment } from './ChatInputAttachments'

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


  // Autocomplete: skill names fetched from /api/skills
  skillNames?: string[]

  // @ file completion: root directory (usually session project_path)
  atRootDir?: string

  // Selected preview element
  selectedTarget?: any
  onClearSelectedTarget?: () => void
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
  skillNames = [],
  atRootDir = '',
  selectedTarget,
  onClearSelectedTarget,
}: ChatInputProps) {
  const { t } = useTranslation()
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const groupRef = useRef<HTMLDivElement>(null)
  const projectRef = useRef<HTMLDivElement>(null)
  const branchRef = useRef<HTMLDivElement>(null)
  const [attachments, setAttachments] = useState<Attachment[]>([])
  const isDesignMode = useRuntimeStore((s) => s.isDesignMode)
  const setDesignMode = useRuntimeStore((s) => s.setDesignMode)
  const setSidebarCollapsed = useRuntimeStore((s) => s.setSidebarCollapsed)

  // ─── Autocomplete ref (access handleInput / handleKeyDown) ───────────────
  const acRef = useRef<ChatInputAutocompleteHandle>(null)

  // ─── Inline highlight (backdrop-textarea) state ───────────────────────────
  const backdropInnerRef = useRef<HTMLDivElement>(null)
  const [inputValue, setInputValue] = useState('')
  // Maps display label (e.g. "src/ChatInput.tsx") → absolute path
  const [atMentions, setAtMentions] = useState<Map<string, string>>(new Map())

  // Selectors State
  const [activeDropdown, setActiveDropdown] = useState<'group' | 'project' | 'branch' | null>(null)
  const [dropdownPos, setDropdownPos] = useState<{ top?: number; bottom?: number; left: number } | null>(null)
  const [branch, setBranch] = useState<string>('main')
  const [branches, setBranches] = useState<string[]>(['main'])

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

  // ─── Callbacks for autocomplete component ────────────────────────────────

  // Called by autocomplete after it modifies textarea content (e.g. applying a selection)
  const handleAutocompleteValueChange = useCallback(() => {
    const el = inputRef.current
    if (!el) return
    autoResize()
    setInputValue(el.value)
  }, [])

  // Called by autocomplete when a file @mention is resolved
  const handleSelectMention = useCallback((path: string, name: string) => {
    setAtMentions(prev => new Map(prev).set(name, path))
  }, [])

  // Context window ring calculation
  const cwPct = useMemo(() => {
    if (!ctxwinLimit || ctxwinLimit <= 0) return 0
    return Math.min(100, Math.max(0, (ctxwinUsed / ctxwinLimit) * 100))
  }, [ctxwinUsed, ctxwinLimit])
  const cwRadius = 7
  const cwCircum = 2 * Math.PI * cwRadius
  const cwOffset = cwCircum - (cwPct / 100) * cwCircum

  // Compute fixed position for dropdown menus (must break out of overflow-x-auto clipping)
  const computeDropdownPos = useCallback(() => {
    if (!activeDropdown) return null
    const ref = activeDropdown === 'group' ? groupRef
              : activeDropdown === 'project' ? projectRef
              : branchRef
    const rect = ref.current?.getBoundingClientRect()
    if (!rect) return null

    const dropdownWidth = activeDropdown === 'project' ? 208
                        : activeDropdown === 'group' ? 176
                        : 128
    const margin = 4
    const vw = window.innerWidth
    const vh = window.innerHeight
    const estimatedHeight = 240

    let left = rect.left
    if (left + dropdownWidth > vw - 8) left = vw - dropdownWidth - 8
    if (left < 8) left = 8

    if (rect.bottom + margin + estimatedHeight > vh) {
      return { top: undefined, bottom: vh - rect.top + margin, left }
    }
    return { top: rect.bottom + margin, bottom: undefined, left }
  }, [activeDropdown])

  useLayoutEffect(() => {
    setDropdownPos(computeDropdownPos())
  }, [computeDropdownPos])

  useEffect(() => {
    if (!activeDropdown) return
    const update = () => setDropdownPos(computeDropdownPos())
    window.addEventListener('resize', update)
    window.addEventListener('scroll', update, true)
    return () => {
      window.removeEventListener('resize', update)
      window.removeEventListener('scroll', update, true)
    }
  }, [activeDropdown, computeDropdownPos])

  // Close dropdown when clicking outside. The dropdown menus are portaled
  // to document.body (see createPortal below), so they are NOT descendants
  // of groupRef/projectRef/branchRef. We also check for the closest
  // [data-dropdown-portal] ancestor so clicks on menu options don't close
  // the menu before their onClick fires (which would happen if we only
  // checked the button wrapper refs).
  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      const targets = [groupRef.current, projectRef.current, branchRef.current]
      const clickedInside = targets.some((ref) => ref && ref.contains(e.target as Node))
      if (clickedInside) return
      const clickedInPortal = (e.target as HTMLElement)?.closest?.('[data-dropdown-portal]')
      if (clickedInPortal) return
      setActiveDropdown(null)
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

  useEffect(() => {
    const handleFill = (e: Event) => {
      const customEvent = e as CustomEvent<string>;
      const text = customEvent.detail;
      if (inputRef.current) {
        inputRef.current.value = text;
        setInputValue(text);
        autoResize();
        inputRef.current.focus();
      }
    };
    window.addEventListener('fill-chat-input', handleFill);
    return () => window.removeEventListener('fill-chat-input', handleFill);
  }, []);

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
            toast.error(t('common.failedToUploadImage'))
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

    // Intercept /cancel immediately while streaming — don't queue
    if (rawText.toLowerCase() === '/cancel' && (streaming || processing)) {
      onCancel()
      if (inputRef.current) inputRef.current.value = ''
      setInputValue('')
      setAtMentions(new Map())
      attachments.forEach((att) => URL.revokeObjectURL(att.previewUrl))
      setAttachments([])
      if (inputRef.current) inputRef.current.style.height = 'auto'
      return
    }

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
  }, [disabled, onSend, onCancel, streaming, processing, attachments, selectedGroup, selectedProjectPath, atMentions])

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.nativeEvent.isComposing) return

    // Delegate autocomplete keyboard navigation first
    if (acRef.current?.handleKeyDown(e)) return

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

  // Update input state and delegate autocomplete detection on every input change
  const handleInput = useCallback(() => {
    autoResize()
    const el = inputRef.current
    if (!el) return
    setInputValue(el.value)
    acRef.current?.handleInput()
  }, [])


  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-2">
      {/* Input card */}
      <div className={cn(
        "relative flex flex-col rounded-xl border border-border/40 bg-background p-2.5 transition-all shadow-sm focus-within:border-primary/30 focus-within:shadow-md"
      )}>
          {/* Attachments preview & Selected Element Badge */}
          <ChatInputAttachments
            attachments={attachments}
            selectedTarget={selectedTarget}
            onClearSelectedTarget={onClearSelectedTarget}
            onRemove={removeAttachment}
          />

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
              {/* ── Autocomplete popups (/ commands + @ files) ── */}
              <ChatInputAutocomplete
                ref={acRef}
                value={inputValue}
                inputRef={inputRef}
                skillNames={skillNames}
                atRootDir={atRootDir}
                onSelectMention={handleSelectMention}
                onValueChange={handleAutocompleteValueChange}
              />

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
                placeholder={disabled ? "" : "Ask anything..."}
                rows={1}
                disabled={disabled}
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
                  onClick={() => toast.info(t('common.dragDropImages'))}
                  className="p-1.5 rounded-lg hover:bg-muted text-muted-foreground/70 transition-colors cursor-pointer shrink-0 hidden sm:block"
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
                    "p-1.5 rounded-lg transition-colors cursor-pointer shrink-0 hidden sm:block",
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

                      {activeDropdown === 'group' && groups.length > 0 && createPortal(
                        <div
                          data-dropdown-portal
                          className="fixed z-50 w-44 rounded-xl border border-border bg-popover p-1 shadow-lg max-h-60 overflow-y-auto"
                          style={dropdownPos ? { top: dropdownPos.top !== undefined ? `${dropdownPos.top}px` : undefined, bottom: dropdownPos.bottom !== undefined ? `${dropdownPos.bottom}px` : undefined, left: `${dropdownPos.left}px` } : undefined}
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
                        </div>,
                        document.body
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

                          {activeDropdown === 'project' && createPortal(
                            <div
                              data-dropdown-portal
                              className="fixed z-50 w-52 rounded-xl border border-border bg-popover p-1 shadow-lg max-h-60 overflow-y-auto"
                              style={dropdownPos ? { top: dropdownPos.top !== undefined ? `${dropdownPos.top}px` : undefined, bottom: dropdownPos.bottom !== undefined ? `${dropdownPos.bottom}px` : undefined, left: `${dropdownPos.left}px` } : undefined}
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
                            </div>,
                            document.body
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

                          {activeDropdown === 'branch' && createPortal(
                            <div
                              data-dropdown-portal
                              className="fixed z-50 w-32 rounded-xl border border-border bg-popover p-1 shadow-lg"
                              style={dropdownPos ? { top: dropdownPos.top !== undefined ? `${dropdownPos.top}px` : undefined, bottom: dropdownPos.bottom !== undefined ? `${dropdownPos.bottom}px` : undefined, left: `${dropdownPos.left}px` } : undefined}
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
                            </div>,
                            document.body
                          )}
                        </div>
                      </>
                    )}
                  </div>
                )}
              </div>

              {/* Right actions: model badge, context window ring, send/stop */}
              <div className="flex items-center gap-2">


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
                        className="text-signal transition-all duration-500 ease-out"
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

                {(streaming || delegating || processing) && !inputValue.trim() ? (
                  <button
                    type="button"
                    onClick={onCancel}
                    className="flex items-center gap-1 px-2 sm:px-3 py-1 rounded-xl bg-destructive/10 text-destructive hover:bg-destructive/20 transition-all text-xs font-semibold cursor-pointer"
                  >
                    <StopCircle className="h-3.5 w-3.5" />
                    <span className="hidden sm:inline">{t('common.stopped')}</span>
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
