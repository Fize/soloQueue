import { type KeyboardEvent, useRef, useEffect, useCallback, useState, useMemo } from 'react'
import { toast } from 'sonner'
import { 
  ArrowUp, StopCircle, X, Loader2, Plus, ChevronDown, 
  Check, Laptop, GitBranch, Users
} from 'lucide-react'
import { uploadFile, getProjectBranches } from '@/lib/api'
import type { Project } from '@/types'
import { cn } from '@/lib/utils'

export interface ChatInputProps {
  onSend: (
    text: string, 
    files?: { name: string; path: string }[],
    group?: string,
    projectPath?: string
  ) => void
  onCancel: () => void
  streaming: boolean
  delegating: boolean
  disabled: boolean
  activeSessionId?: string

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
}: ChatInputProps) {
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const groupRef = useRef<HTMLDivElement>(null)
  const projectRef = useRef<HTMLDivElement>(null)
  const branchRef = useRef<HTMLDivElement>(null)
  const [attachments, setAttachments] = useState<Attachment[]>([])

  // Selectors State
  const [activeDropdown, setActiveDropdown] = useState<'group' | 'project' | 'branch' | null>(null)
  const [dropdownPos, setDropdownPos] = useState<{ bottom: number; left: number } | null>(null)
  const [branch, setBranch] = useState<string>('main')
  const [branches, setBranches] = useState<string[]>(['main'])

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
    const text = inputRef.current?.value.trim() || ''

    // Block sending if there are uploads in progress
    const hasUploading = attachments.some((att) => att.status === 'uploading')
    if (hasUploading) return

    const uploadedFiles = attachments
      .filter((att) => att.status === 'done' && att.path)
      .map((att) => ({ name: att.name, path: att.path! }))

    if ((!text && uploadedFiles.length === 0) || (streaming && !delegating) || disabled) return

    // Fallback prompt to satisfy backend non-empty check
    const finalPrompt =
      text ||
      (uploadedFiles.length === 1 ? `Pasted image: ${uploadedFiles[0].name}` : 'Pasted images')

    onSend(
      finalPrompt, 
      uploadedFiles.length > 0 ? uploadedFiles : undefined,
      selectedGroup || undefined,
      selectedProjectPath || undefined
    )

    if (inputRef.current) inputRef.current.value = ''

    // Clear and revoke attachments
    attachments.forEach((att) => URL.revokeObjectURL(att.previewUrl))
    setAttachments([])

    // Reset height
    if (inputRef.current) inputRef.current.style.height = 'auto'
  }, [streaming, disabled, onSend, attachments, selectedGroup, selectedProjectPath, delegating])

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
    const nextHeight = Math.min(el.scrollHeight, 160)
    el.style.height = nextHeight + 'px'
  }



  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-2">
      {/* Main card wrapper */}
      <div className="relative flex flex-col rounded-3xl border border-border/60 bg-muted/20 dark:bg-muted/10 p-2 shadow-sm transition-all focus-within:shadow-md focus-within:border-primary/30">
        
        {/* Upper Card Area: input text area */}
        <div className="w-full bg-background rounded-2xl border border-border/40 shadow-sm flex flex-col p-2.5">
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

          <div className="flex flex-col w-full min-h-[32px]">
            <textarea
              ref={inputRef}
              className="w-full resize-none bg-transparent px-3 py-1 text-[15px] leading-normal text-foreground placeholder:text-muted-foreground/45 focus:outline-none min-h-[32px] max-h-[160px] overflow-y-auto rounded-lg"
              placeholder="Ask anything..."
              rows={1}
              disabled={(streaming && !delegating) || disabled}
              onKeyDown={handleKeyDown}
              onInput={autoResize}
              onPaste={handlePaste}
            />

            {/* Inner action buttons row */}
            <div className="flex items-center justify-between mt-2 pt-2 border-t border-border/30">
              {/* Left actions: plus and selectors */}
              <div className="flex items-center gap-2 flex-1 min-w-0">
                <button
                  type="button"
                  onClick={() => toast.info('Drag and drop or paste images to attach')}
                  className="p-1.5 rounded-lg hover:bg-muted text-muted-foreground/70 transition-colors cursor-pointer shrink-0"
                  title="Add context"
                >
                  <Plus className="h-4 w-4" />
                </button>

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
                        <Users className="h-3 w-3 text-muted-foreground/60" />
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
                            <Laptop className="h-3 w-3 text-muted-foreground/60" />
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
                            <GitBranch className="h-3 w-3 text-muted-foreground/60" />
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

              {/* Right actions: context window ring, send/stop */}
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
                        className="text-violet-500 transition-all duration-500 ease-out"
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

                {streaming && !delegating ? (
                  <button
                    type="button"
                    onClick={onCancel}
                    className="flex items-center gap-1 px-3 py-1 rounded-xl bg-destructive/10 text-destructive hover:bg-destructive/20 transition-all text-xs font-semibold cursor-pointer"
                  >
                    <StopCircle className="h-4 w-4" />
                    <span>Stop</span>
                  </button>
                ) : (
                  <button
                    type="button"
                    onClick={handleSubmit}
                    disabled={disabled || attachments.some((att) => att.status === 'uploading')}
                    className="flex items-center justify-center h-8 w-8 rounded-full bg-zinc-800 dark:bg-zinc-200 text-zinc-100 dark:text-zinc-900 hover:opacity-90 transition-all disabled:opacity-20 disabled:cursor-not-allowed cursor-pointer"
                  >
                    <ArrowUp className="h-4 w-4 stroke-[2.5]" />
                  </button>
                )}
              </div>
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
