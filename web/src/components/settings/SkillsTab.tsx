import { useEffect, useState, useMemo } from 'react'
import { useToolsAndSkillsStore } from '@/stores/toolsAndSkillsStore'
import {
  installSkill,
  deleteSkill,
  updateSkill,
  importSkill,
  fetchSkillDetail,
  fetchSkillFiles,
  toggleSkill,
  type SkillFileEntry,
} from '@/lib/api'
import type { SkillInfo } from '@/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Sparkles,
  BookOpen,
  Plus,
  Trash2,
  Loader2,
  ChevronDown,
  ChevronUp,
  Folder,
  FileText,
  Globe,
  Link as LinkIcon,
  RefreshCw,
  Search,
  Check,
  Download,
  AlertTriangle,
} from 'lucide-react'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { cn } from '@/lib/utils'

// Depth indent helper for file listing
function depthIndent(p: string): number {
  const depth = Math.min(4, p.split('/').length - 1)
  return depth * 12
}

// Get file name from path
function leafName(p: string): string {
  const idx = p.lastIndexOf('/')
  return idx >= 0 ? p.slice(idx + 1) : p
}

// Size formatting helper
function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

export function SkillsTab() {
  const skills = useToolsAndSkillsStore((state) => state.skills)
  const skillsLoading = useToolsAndSkillsStore((state) => state.skillsLoading)
  const fetchSkills = useToolsAndSkillsStore((state) => state.fetchSkills)

  const storeSkills = useToolsAndSkillsStore((state) => state.storeSkills)
  const storeSkillsLoading = useToolsAndSkillsStore((state) => state.storeSkillsLoading)
  const fetchStoreSkills = useToolsAndSkillsStore((state) => state.fetchStoreSkills)

  // Sub-tabs state
  const [activeSubTab, setActiveSubTab] = useState<'installed' | 'store'>('installed')

  // Search & Filter state
  const [searchQuery, setSearchQuery] = useState('')
  const [categoryFilter, setCategoryFilter] = useState<'all' | 'builtin' | 'user'>('all')
  const [storeSearchQuery, setStoreSearchQuery] = useState('')

  // Lazy loading details & file lists
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [details, setDetails] = useState<Record<string, SkillInfo>>({})
  const [loadingDetails, setLoadingDetails] = useState<Record<string, boolean>>({})
  const [files, setFiles] = useState<Record<string, SkillFileEntry[]>>({})
  const [loadingFiles, setLoadingFiles] = useState<Record<string, boolean>>({})

  // Interactive operation states
  const [togglingId, setTogglingId] = useState<string | null>(null)
  const [installingStoreId, setInstallingStoreId] = useState<string | null>(null)

  // Custom install fields
  const [customGitUrl, setCustomGitUrl] = useState('')
  const [customLocalPath, setCustomLocalPath] = useState('')
  const [installingCustom, setInstallingCustom] = useState(false)
  const [customInstallError, setCustomInstallError] = useState<string | null>(null)

  // Edit / Creation form states
  const [editId, setEditId] = useState<string | null>(null)
  const [editDesc, setEditDesc] = useState('')
  const [editBody, setEditBody] = useState('')
  const [editTriggers, setEditTriggers] = useState('')
  const [editSaving, setEditSaving] = useState(false)
  const [editError, setEditError] = useState<string | null>(null)
  const [activeEditPaneTab, setActiveEditPaneTab] = useState<'preview' | 'edit'>('preview')

  // Import Dialog states
  const [importDialogOpen, setImportDialogOpen] = useState(false)
  const [importName, setImportName] = useState('')
  const [importDesc, setImportDesc] = useState('')
  const [importTriggers, setImportTriggers] = useState('')
  const [importBody, setImportBody] = useState('')
  const [importSaving, setImportSaving] = useState(false)
  const [importError, setImportError] = useState<string | null>(null)

  // Initialization
  useEffect(() => {
    fetchSkills()
    fetchStoreSkills()
  }, [fetchSkills, fetchStoreSkills])

  // Lazy load handles
  const handleToggleExpand = async (id: string) => {
    if (expandedId === id) {
      setExpandedId(null)
      return
    }

    setExpandedId(id)
    setActiveEditPaneTab('preview')
    setEditId(null) // Abort edit on other/same rows

    // Fetch Details
    if (!details[id]) {
      setLoadingDetails((prev) => ({ ...prev, [id]: true }))
      try {
        const detail = await fetchSkillDetail(id)
        setDetails((prev) => ({ ...prev, [id]: detail }))
      } catch (err) {
        console.error('Failed to fetch skill details:', err)
      } finally {
        setLoadingDetails((prev) => ({ ...prev, [id]: false }))
      }
    }

    // Fetch Files list
    if (!files[id]) {
      setLoadingFiles((prev) => ({ ...prev, [id]: true }))
      try {
        const fileList = await fetchSkillFiles(id)
        setFiles((prev) => ({ ...prev, [id]: fileList.files || [] }))
      } catch (err) {
        console.error('Failed to fetch skill files:', err)
      } finally {
        setLoadingFiles((prev) => ({ ...prev, [id]: false }))
      }
    }
  }

  // Toggling Enabled/Disabled
  const handleToggleEnable = async (id: string) => {
    setTogglingId(id)
    try {
      await toggleSkill(id)
      await fetchSkills()
    } catch (err) {
      console.error('Failed to toggle skill:', err)
    } finally {
      setTogglingId(null)
    }
  }

  // Delete / Uninstall
  const handleDelete = async (id: string) => {
    if (
      !window.confirm(
        `Uninstall skill "${id}"? This will delete its folder from user skills directory.`
      )
    ) {
      return
    }
    try {
      await deleteSkill(id)
      if (expandedId === id) setExpandedId(null)
      if (editId === id) setEditId(null)
      await fetchSkills()
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to uninstall skill')
    }
  }

  // Start Editing
  const handleStartEdit = (skill: SkillInfo) => {
    const cachedBody = details[skill.id]?.body || skill.body || ''
    const cachedTriggers = (details[skill.id]?.triggers || skill.triggers || []).join(', ')
    setEditId(skill.id)
    setEditDesc(skill.description || '')
    setEditBody(cachedBody)
    setEditTriggers(cachedTriggers)
    setEditError(null)
    setActiveEditPaneTab('edit')
  }

  // Cancel Editing
  const handleCancelEdit = () => {
    setEditId(null)
    setActiveEditPaneTab('preview')
  }

  // Save Edits
  const handleSaveEdit = async (id: string) => {
    setEditSaving(true)
    setEditError(null)
    try {
      const triggersArr = editTriggers
        .split(',')
        .map((t) => t.trim())
        .filter(Boolean)

      await updateSkill(id, {
        description: editDesc,
        body: editBody,
        triggers: triggersArr,
      })

      // Update cached values
      const updatedDetail = await fetchSkillDetail(id)
      setDetails((prev) => ({ ...prev, [id]: updatedDetail }))

      // Update files list (SKILL.md might have changed or been created)
      const fileList = await fetchSkillFiles(id)
      setFiles((prev) => ({ ...prev, [id]: fileList.files || [] }))

      await fetchSkills()
      setEditId(null)
      setActiveEditPaneTab('preview')
    } catch (err) {
      setEditError(err instanceof Error ? err.message : 'Failed to update skill')
    } finally {
      setEditSaving(false)
    }
  }

  // Import New Skill
  const handleCreateSkill = async () => {
    if (!importName.trim()) {
      setImportError('Skill ID is required')
      return
    }
    if (!importBody.trim()) {
      setImportError('Instructions body content (Markdown) is required')
      return
    }

    setImportSaving(true)
    setImportError(null)
    try {
      const triggersArr = importTriggers
        .split(',')
        .map((t) => t.trim())
        .filter(Boolean)

      await importSkill({
        name: importName.trim(),
        description: importDesc.trim(),
        body: importBody.trim(),
        triggers: triggersArr,
      })

      setImportName('')
      setImportDesc('')
      setImportTriggers('')
      setImportBody('')
      setImportDialogOpen(false)
      await fetchSkills()
    } catch (err) {
      setImportError(err instanceof Error ? err.message : 'Failed to create skill')
    } finally {
      setImportSaving(false)
    }
  }

  // Install from Store Catalog
  const handleInstallFromStore = async (id: string) => {
    setInstallingStoreId(id)
    try {
      await installSkill({ source: 'store', id })
      await Promise.all([fetchSkills(), fetchStoreSkills()])
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to install skill')
    } finally {
      setInstallingStoreId(null)
    }
  }

  // Custom Github Installation
  const handleInstallGit = async () => {
    if (!customGitUrl.trim()) return
    setInstallingCustom(true)
    setCustomInstallError(null)
    try {
      await installSkill({ source: 'github', url: customGitUrl.trim() })
      setCustomGitUrl('')
      await Promise.all([fetchSkills(), fetchStoreSkills()])
      alert('Skill installed successfully via Git cloning!')
    } catch (err) {
      setCustomInstallError(err instanceof Error ? err.message : 'Failed to clone git repository')
    } finally {
      setInstallingCustom(false)
    }
  }

  // Custom Local Link Installation
  const handleInstallLocal = async () => {
    if (!customLocalPath.trim()) return
    setInstallingCustom(true)
    setCustomInstallError(null)
    try {
      await installSkill({ source: 'local', path: customLocalPath.trim() })
      setCustomLocalPath('')
      await Promise.all([fetchSkills(), fetchStoreSkills()])
      alert('Skill symlinked successfully from local path!')
    } catch (err) {
      setCustomInstallError(err instanceof Error ? err.message : 'Failed to link local directory')
    } finally {
      setInstallingCustom(false)
    }
  }

  // Filtering lists
  const filteredSkills = useMemo(() => {
    const list = skills?.skills ?? []
    const q = searchQuery.toLowerCase().trim()
    return list.filter((s) => {
      if (categoryFilter !== 'all' && s.category !== categoryFilter) return false
      if (!q) return true
      const matchesId = s.id.toLowerCase().includes(q)
      const matchesName = s.name.toLowerCase().includes(q)
      const matchesDesc = (s.description || '').toLowerCase().includes(q)
      const matchesTriggers = (s.triggers || []).some((t) => t.toLowerCase().includes(q))
      return matchesId || matchesName || matchesDesc || matchesTriggers
    })
  }, [skills, searchQuery, categoryFilter])

  const filteredStoreSkills = useMemo(() => {
    const list = storeSkills?.skills ?? []
    const q = storeSearchQuery.toLowerCase().trim()
    return list.filter((s) => {
      if (!q) return true
      const matchesId = s.id.toLowerCase().includes(q)
      const matchesName = s.name.toLowerCase().includes(q)
      const matchesDesc = (s.description || '').toLowerCase().includes(q)
      const matchesTriggers = (s.triggers || []).some((t) => t.toLowerCase().includes(q))
      return matchesId || matchesName || matchesDesc || matchesTriggers
    })
  }, [storeSkills, storeSearchQuery])

  // Helpers to count
  const installedCount = skills?.total ?? 0
  const storeCount = storeSkills?.total ?? 0

  return (
    <div className="space-y-6 text-left">
      {/* ── Sub Tabs Navigation ── */}
      <div className="flex border-b border-border">
        <button
          onClick={() => setActiveSubTab('installed')}
          className={cn(
            'flex items-center gap-1.5 px-4 py-2.5 text-sm font-semibold border-b-2 transition-all',
            activeSubTab === 'installed'
              ? 'border-primary text-foreground'
              : 'border-transparent text-muted-foreground hover:text-foreground'
          )}
        >
          <Sparkles className="h-4 w-4" />
          Installed Skills
          {installedCount > 0 && (
            <Badge variant="secondary" className="ml-1 text-[10px]">
              {installedCount}
            </Badge>
          )}
        </button>
        <button
          onClick={() => setActiveSubTab('store')}
          className={cn(
            'flex items-center gap-1.5 px-4 py-2.5 text-sm font-semibold border-b-2 transition-all',
            activeSubTab === 'store'
              ? 'border-primary text-foreground'
              : 'border-transparent text-muted-foreground hover:text-foreground'
          )}
        >
          <Globe className="h-4 w-4" />
          Skill Store
          {storeCount > 0 && (
            <Badge variant="secondary" className="ml-1 text-[10px]">
              {storeCount}
            </Badge>
          )}
        </button>
      </div>

      {/* ── Tab Content: Installed Skills ── */}
      {activeSubTab === 'installed' && (
        <div className="space-y-4">
          {/* Toolbar */}
          <div className="flex flex-col sm:flex-row gap-3 items-stretch sm:items-center justify-between">
            <div className="flex flex-1 gap-2 max-w-lg">
              <div className="relative flex-1">
                <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                <Input
                  className="pl-9 h-9"
                  placeholder="Search installed skills..."
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                />
              </div>
              <div className="w-36">
                <select
                  value={categoryFilter}
                  onChange={(e) => setCategoryFilter(e.target.value as 'all' | 'builtin' | 'user')}
                  className="w-full h-9 rounded-md border border-border bg-card px-3 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary"
                >
                  <option value="all">All Types</option>
                  <option value="builtin">Built-in</option>
                  <option value="user">User Created</option>
                </select>
              </div>
            </div>

            <Button size="sm" className="gap-1.5 h-9" onClick={() => setImportDialogOpen(true)}>
              <Plus className="h-4 w-4" />
              Create Skill
            </Button>
          </div>

          {skillsLoading && (
            <div className="flex items-center justify-center py-12 text-sm text-muted-foreground gap-2">
              <Loader2 className="h-4 w-4 animate-spin" />
              Loading skills...
            </div>
          )}

          {!skillsLoading && filteredSkills.length === 0 && (
            <div className="border border-dashed rounded-lg bg-card/20 p-8 text-center">
              <p className="text-sm text-muted-foreground">
                {searchQuery ? 'No installed skills match your search' : 'No skills installed yet.'}
              </p>
              {!searchQuery && (
                <Button
                  variant="outline"
                  size="sm"
                  className="mt-3"
                  onClick={() => setActiveSubTab('store')}
                >
                  Browse Store Catalog
                </Button>
              )}
            </div>
          )}

          {/* Collapsible Skill Row List */}
          <div className="space-y-3">
            {filteredSkills.map((skill) => {
              const isExpanded = expandedId === skill.id
              const isEditing = editId === skill.id
              const isBuiltin = skill.category === 'builtin'
              const fileList = files[skill.id] || []
              const bodyContent = details[skill.id]?.body || skill.body || ''

              // Built-in skills can only be uninstalled if they reside in the user directory (meaning overridden/shadowed)
              const isOverridden =
                skill.file_path && skill.file_path.includes('/.soloqueue/skills/')
              const canUninstall = !isBuiltin || isOverridden

              return (
                <div
                  key={skill.id}
                  className={cn(
                    'border rounded-lg bg-card shadow-xs transition-all',
                    isExpanded ? 'ring-1 ring-border shadow-md' : 'hover:shadow-sm'
                  )}
                >
                  {/* Row Header */}
                  <div
                    className={cn(
                      'flex items-center justify-between p-4 cursor-pointer gap-4',
                      isExpanded ? 'border-b border-border bg-muted/10' : ''
                    )}
                    onClick={() => handleToggleExpand(skill.id)}
                  >
                    <div className="flex items-center gap-3 min-w-0 flex-1">
                      <div
                        className={cn(
                          'flex h-8 w-8 shrink-0 items-center justify-center rounded-md',
                          isBuiltin
                            ? 'bg-primary/10 text-primary'
                            : 'bg-[var(--success)]/10 text-[var(--success)]'
                        )}
                      >
                        {isBuiltin ? (
                          <Sparkles className="h-4 w-4" />
                        ) : (
                          <BookOpen className="h-4 w-4" />
                        )}
                      </div>

                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2 flex-wrap">
                          <span className="text-sm font-semibold text-foreground">
                            {skill.name}
                          </span>
                          <span className="text-[10px] font-mono text-muted-foreground/80">
                            ({skill.id})
                          </span>
                          <Badge
                            variant={isBuiltin ? 'primary' : 'success'}
                            className="text-[9px] px-1 py-0 h-4 font-normal"
                          >
                            {isBuiltin ? 'Built-in' : 'User'}
                          </Badge>
                          {skill.context === 'fork' && (
                            <Badge
                              variant="outline"
                              className="text-[9px] px-1 py-0 h-4 font-normal text-amber-500 border-amber-500 bg-amber-500/5"
                            >
                              fork
                            </Badge>
                          )}
                          {!skill.user_invocable && (
                            <Badge
                              variant="outline"
                              className="text-[9px] px-1 py-0 h-4 font-normal text-muted-foreground"
                            >
                              AI only
                            </Badge>
                          )}
                        </div>
                        <p className="mt-0.5 text-xs text-muted-foreground line-clamp-1">
                          {skill.description}
                        </p>
                      </div>
                    </div>

                    {/* Toggle and Expand indicator */}
                    <div
                      className="flex items-center gap-3 shrink-0"
                      onClick={(e) => e.stopPropagation()}
                    >
                      {/* Triggers indicator */}
                      {skill.triggers && skill.triggers.length > 0 && (
                        <div className="hidden md:flex items-center gap-1">
                          {skill.triggers.slice(0, 3).map((trigger: string, i: number) => (
                            <Badge
                              key={i}
                              variant="outline"
                              className="text-[9px] px-1.5 py-0 h-4 font-mono text-muted-foreground max-w-[120px] truncate"
                            >
                              {trigger}
                            </Badge>
                          ))}
                          {skill.triggers.length > 3 && (
                            <span className="text-[9px] text-muted-foreground">
                              +{skill.triggers.length - 3}
                            </span>
                          )}
                        </div>
                      )}

                      {/* Enable Switch Toggle */}
                      <div className="flex items-center gap-2 pr-1">
                        <Label
                          htmlFor={`switch-${skill.id}`}
                          className="text-[10px] text-muted-foreground cursor-pointer hidden sm:inline"
                        >
                          {skill.enabled ? 'Enabled' : 'Disabled'}
                        </Label>
                        {togglingId === skill.id ? (
                          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                        ) : (
                          <Switch
                            id={`switch-${skill.id}`}
                            checked={skill.enabled}
                            onCheckedChange={() => handleToggleEnable(skill.id)}
                          />
                        )}
                      </div>

                      <div
                        className="p-1 rounded-md hover:bg-muted text-muted-foreground cursor-pointer"
                        onClick={() => handleToggleExpand(skill.id)}
                      >
                        {isExpanded ? (
                          <ChevronUp className="h-4 w-4" />
                        ) : (
                          <ChevronDown className="h-4 w-4" />
                        )}
                      </div>
                    </div>
                  </div>

                  {/* Expanded Body Panel */}
                  {isExpanded && (
                    <div className="p-4 grid grid-cols-1 lg:grid-cols-3 gap-6 text-left">
                      {/* Left: Files Tree */}
                      <div className="lg:col-span-1 space-y-2">
                        <h4 className="text-xs font-bold text-foreground flex items-center gap-1.5 border-b border-border pb-1.5">
                          <Folder className="h-3.5 w-3.5 text-primary" />
                          Skill Directory Files
                        </h4>

                        {loadingFiles[skill.id] && (
                          <div className="flex items-center justify-center py-8 text-xs text-muted-foreground gap-1.5">
                            <Loader2 className="h-3 w-3 animate-spin" />
                            Loading files...
                          </div>
                        )}

                        {!loadingFiles[skill.id] && fileList.length === 0 && (
                          <div className="py-8 text-center text-xs text-muted-foreground">
                            No files found in skill folder.
                          </div>
                        )}

                        {!loadingFiles[skill.id] && fileList.length > 0 && (
                          <div className="max-h-[350px] overflow-y-auto border border-border rounded-md bg-muted/10 p-2 font-mono text-[11px] space-y-0.5">
                            {fileList.map((entry) => (
                              <div
                                key={entry.path}
                                className="flex items-center justify-between py-1 px-1.5 rounded hover:bg-muted/40 text-muted-foreground hover:text-foreground"
                                style={{ paddingLeft: `${depthIndent(entry.path) + 6}px` }}
                              >
                                <div className="flex items-center gap-1.5 min-w-0">
                                  {entry.kind === 'directory' ? (
                                    <Folder className="h-3.5 w-3.5 text-blue-400 shrink-0" />
                                  ) : (
                                    <FileText className="h-3.5 w-3.5 text-purple-400 shrink-0" />
                                  )}
                                  <span className="truncate">{leafName(entry.path)}</span>
                                </div>
                                {entry.kind === 'file' && typeof entry.size === 'number' && (
                                  <span className="text-[9px] text-muted-foreground/60 shrink-0 ml-2">
                                    {formatSize(entry.size)}
                                  </span>
                                )}
                              </div>
                            ))}
                          </div>
                        )}

                        {/* File path help */}
                        {skill.file_path && (
                          <p
                            className="text-[10px] text-muted-foreground truncate"
                            title={skill.file_path}
                          >
                            Path: <span className="font-mono">{skill.file_path}</span>
                          </p>
                        )}
                      </div>

                      {/* Right: Preview & Editor Tabs */}
                      <div className="lg:col-span-2 flex flex-col min-h-[300px]">
                        <div className="flex items-center justify-between border-b border-border pb-1.5 mb-3">
                          {/* Inner Tabs toggles */}
                          <div className="flex rounded-md bg-muted p-0.5 border border-border">
                            <button
                              type="button"
                              onClick={() => {
                                setActiveEditPaneTab('preview')
                                setEditId(null)
                              }}
                              className={cn(
                                'rounded-[4px] px-2.5 py-1 text-xs font-semibold transition-all',
                                activeEditPaneTab === 'preview' && !isEditing
                                  ? 'bg-background text-foreground shadow-xs'
                                  : 'text-muted-foreground hover:text-foreground'
                              )}
                            >
                              Readme
                            </button>
                            <button
                              type="button"
                              onClick={() => handleStartEdit(skill)}
                              className={cn(
                                'rounded-[4px] px-2.5 py-1 text-xs font-semibold transition-all',
                                activeEditPaneTab === 'edit' || isEditing
                                  ? 'bg-background text-foreground shadow-xs'
                                  : 'text-muted-foreground hover:text-foreground'
                              )}
                            >
                              Edit / Override
                            </button>
                          </div>

                          {/* Uninstall/Delete action */}
                          {canUninstall && (
                            <Button
                              variant="ghost"
                              size="xs"
                              className="text-destructive hover:bg-destructive/10 gap-1"
                              onClick={() => handleDelete(skill.id)}
                            >
                              <Trash2 className="h-3 w-3" />
                              {isBuiltin ? 'Remove Override' : 'Uninstall'}
                            </Button>
                          )}
                        </div>

                        {/* Loading details indicator */}
                        {loadingDetails[skill.id] && (
                          <div className="flex-1 flex items-center justify-center py-12 text-sm text-muted-foreground gap-2">
                            <Loader2 className="h-4 w-4 animate-spin" />
                            Loading details...
                          </div>
                        )}

                        {/* Tab Content: Preview Readme */}
                        {activeEditPaneTab === 'preview' && !loadingDetails[skill.id] && (
                          <div className="flex-1 max-h-[350px] overflow-y-auto border border-border rounded-md bg-card p-3 prose prose-sm dark:prose-invert">
                            {bodyContent ? (
                              <MarkdownPreview content={bodyContent} />
                            ) : (
                              <span className="text-xs text-muted-foreground italic">
                                No instructions defined.
                              </span>
                            )}
                          </div>
                        )}

                        {/* Tab Content: Edit Form */}
                        {(activeEditPaneTab === 'edit' || isEditing) &&
                          !loadingDetails[skill.id] && (
                            <div className="flex-1 flex flex-col gap-3">
                              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                                <div className="flex flex-col gap-1">
                                  <Label className="text-xs">Triggers (comma-separated)</Label>
                                  <Input
                                    value={editTriggers}
                                    onChange={(e) => setEditTriggers(e.target.value)}
                                    placeholder="e.g. search web, summarize"
                                    className="h-8 text-xs"
                                  />
                                </div>
                                <div className="flex flex-col gap-1">
                                  <Label className="text-xs">Description</Label>
                                  <Input
                                    value={editDesc}
                                    onChange={(e) => setEditDesc(e.target.value)}
                                    placeholder="Skill purpose"
                                    className="h-8 text-xs"
                                  />
                                </div>
                              </div>

                              <div className="flex flex-col gap-1 flex-1">
                                <Label className="text-xs">SKILL.md Markdown Content</Label>
                                <textarea
                                  value={editBody}
                                  onChange={(e) => setEditBody(e.target.value)}
                                  rows={10}
                                  className="w-full rounded-md border border-border bg-[#1E1E2E] px-3 py-2 font-mono text-xs text-[#E5E7EB] transition-colors outline-none focus-visible:border-primary focus-visible:ring-1 focus-visible:ring-ring/50 resize-y flex-1"
                                  placeholder="# Instructions title"
                                  spellCheck={false}
                                />
                              </div>

                              {editError && (
                                <p className="text-xs text-destructive flex items-center gap-1">
                                  <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
                                  {editError}
                                </p>
                              )}

                              {isBuiltin && !isOverridden && (
                                <div className="rounded border border-amber-500/20 bg-amber-500/5 p-2 text-[10px] text-amber-500/80 leading-normal flex items-start gap-1.5">
                                  <AlertTriangle className="h-3.5 w-3.5 shrink-0 mt-0.5" />
                                  <span>
                                    <strong>Note:</strong> Editing this built-in skill will create a
                                    custom override file inside your user directory, leaving the
                                    original built-in skill intact.
                                  </span>
                                </div>
                              )}

                              <div className="flex justify-end gap-2 mt-1">
                                <Button
                                  variant="outline"
                                  size="xs"
                                  onClick={handleCancelEdit}
                                  disabled={editSaving}
                                >
                                  Cancel
                                </Button>
                                <Button
                                  size="xs"
                                  onClick={() => handleSaveEdit(skill.id)}
                                  disabled={editSaving}
                                >
                                  {editSaving ? (
                                    <>
                                      <Loader2 className="mr-1 h-3 w-3 animate-spin" />
                                      Saving...
                                    </>
                                  ) : (
                                    'Save Changes'
                                  )}
                                </Button>
                              </div>
                            </div>
                          )}
                      </div>
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* ── Tab Content: Skill Store ── */}
      {activeSubTab === 'store' && (
        <div className="space-y-6">
          {/* Header catalog tip */}
          <div className="rounded-lg border border-border p-4 bg-muted/10">
            <h4 className="text-sm font-bold text-foreground">Skill Store Catalog</h4>
            <p className="text-xs text-muted-foreground mt-1 leading-relaxed">
              Install pre-built skill templates from the catalog, pull skill packages directly from
              remote Git repositories, or symlink local folder trees.
            </p>
          </div>

          {/* Search bar */}
          <div className="flex gap-2 max-w-md">
            <div className="relative flex-1">
              <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                className="pl-9 h-9"
                placeholder="Search store catalog..."
                value={storeSearchQuery}
                onChange={(e) => setStoreSearchQuery(e.target.value)}
              />
            </div>
          </div>

          {storeSkillsLoading && (
            <div className="flex items-center justify-center py-12 text-sm text-muted-foreground gap-2">
              <Loader2 className="h-4 w-4 animate-spin" />
              Loading store skills...
            </div>
          )}

          {/* Catalog grid */}
          {!storeSkillsLoading && (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              {filteredStoreSkills.map((s: SkillInfo) => {
                // Determine if this skill catalog is already installed
                const isInstalled = skills?.skills.some((installed) => installed.id === s.id)

                return (
                  <div
                    key={s.id}
                    className="border rounded-lg bg-card p-4 flex flex-col justify-between hover:shadow-sm"
                  >
                    <div>
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-sm font-semibold text-foreground">{s.name}</span>
                        <span className="text-[10px] font-mono text-muted-foreground/60">
                          {s.id}
                        </span>
                      </div>
                      <p className="text-xs text-muted-foreground/80 mt-1.5 line-clamp-3">
                        {s.description || 'No description provided.'}
                      </p>

                      {s.triggers && s.triggers.length > 0 && (
                        <div className="mt-3 flex flex-wrap gap-1">
                          {s.triggers.map((trigger: string, i: number) => (
                            <Badge
                              key={i}
                              variant="outline"
                              className="text-[9px] px-1.5 py-0 h-4 font-mono text-muted-foreground"
                            >
                              {trigger}
                            </Badge>
                          ))}
                        </div>
                      )}
                    </div>

                    <div className="mt-4 pt-3 border-t border-border flex justify-end">
                      {isInstalled ? (
                        <Button
                          variant="outline"
                          size="sm"
                          disabled
                          className="gap-1 text-muted-foreground bg-muted/20"
                        >
                          <Check className="h-3.5 w-3.5" />
                          Installed
                        </Button>
                      ) : (
                        <Button
                          size="sm"
                          variant="secondary"
                          className="gap-1"
                          onClick={() => handleInstallFromStore(s.id)}
                          disabled={installingStoreId === s.id}
                        >
                          {installingStoreId === s.id ? (
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          ) : (
                            <Download className="h-3.5 w-3.5" />
                          )}
                          Install
                        </Button>
                      )}
                    </div>
                  </div>
                )
              })}

              {!storeSkillsLoading && filteredStoreSkills.length === 0 && (
                <div className="col-span-2 border border-dashed rounded-lg py-8 text-center text-xs text-muted-foreground">
                  No catalog skills found matching your search.
                </div>
              )}
            </div>
          )}

          {/* Custom Github / Local installs */}
          <div className="border rounded-lg bg-card shadow-xs overflow-hidden">
            <div className="px-5 py-4 border-b border-border bg-muted/10">
              <h4 className="text-sm font-bold text-foreground">Install Custom Skills</h4>
              <p className="text-xs text-muted-foreground mt-0.5">
                Link local workspaces or import directly from external sources.
              </p>
            </div>

            <div className="p-5 space-y-5 text-left">
              {/* Git install */}
              <div className="grid grid-cols-1 md:grid-cols-4 gap-4 items-end">
                <div className="md:col-span-3 flex flex-col gap-1.5">
                  <Label
                    htmlFor="git-url"
                    className="text-xs font-semibold flex items-center gap-1"
                  >
                    <Globe className="h-3.5 w-3.5 text-muted-foreground" />
                    Install from Git Repository
                  </Label>
                  <Input
                    id="git-url"
                    placeholder="https://github.com/username/soloqueue-skill-example"
                    value={customGitUrl}
                    onChange={(e) => setCustomGitUrl(e.target.value)}
                  />
                  <span className="text-[10px] text-muted-foreground">
                    URL of repository containing a valid SKILL.md file.
                  </span>
                </div>
                <div>
                  <Button
                    onClick={handleInstallGit}
                    className="w-full gap-1.5 h-9"
                    variant="outline"
                    disabled={installingCustom || !customGitUrl.trim()}
                  >
                    {installingCustom ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <RefreshCw className="h-3.5 w-3.5" />
                    )}
                    Clone & Install
                  </Button>
                </div>
              </div>

              <div className="border-t border-border" />

              {/* Local path install */}
              <div className="grid grid-cols-1 md:grid-cols-4 gap-4 items-end">
                <div className="md:col-span-3 flex flex-col gap-1.5">
                  <Label
                    htmlFor="local-path"
                    className="text-xs font-semibold flex items-center gap-1"
                  >
                    <LinkIcon className="h-3.5 w-3.5 text-muted-foreground" />
                    Link Local Directory
                  </Label>
                  <Input
                    id="local-path"
                    placeholder="/Users/username/my-local-skill"
                    value={customLocalPath}
                    onChange={(e) => setCustomLocalPath(e.target.value)}
                  />
                  <span className="text-[10px] text-muted-foreground">
                    Absolute filesystem path of a folder containing a SKILL.md.
                  </span>
                </div>
                <div>
                  <Button
                    onClick={handleInstallLocal}
                    className="w-full gap-1.5 h-9"
                    variant="outline"
                    disabled={installingCustom || !customLocalPath.trim()}
                  >
                    {installingCustom ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <LinkIcon className="h-3.5 w-3.5" />
                    )}
                    Symlink Skill
                  </Button>
                </div>
              </div>

              {customInstallError && (
                <div className="rounded-md border border-destructive/50 bg-destructive/10 px-4 py-2 text-xs text-destructive flex items-center gap-2">
                  <AlertTriangle className="h-4 w-4 shrink-0" />
                  {customInstallError}
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* ── Import/Creation Dialog ── */}
      <Dialog open={importDialogOpen} onOpenChange={setImportDialogOpen}>
        <DialogContent className="md:max-w-3xl w-[95vw] max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Create Custom Skill</DialogTitle>
            <DialogDescription>
              Write details and instructions for a new AI skill. It will be stored inside your user
              skills directory.
            </DialogDescription>
          </DialogHeader>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4 my-2 text-left">
            <div className="flex flex-col gap-3">
              <div className="flex flex-col gap-1.5">
                <Input
                  label="Skill ID / Folder name"
                  value={importName}
                  onChange={(e) => setImportName(e.target.value)}
                  placeholder="e.g. search-web"
                />
                <span className="text-[10px] text-muted-foreground">
                  Allowed characters: a-z, 0-9, dash, underscore.
                </span>
              </div>

              <div className="flex flex-col gap-1.5">
                <Label>Description</Label>
                <textarea
                  value={importDesc}
                  onChange={(e) => setImportDesc(e.target.value)}
                  rows={2}
                  className="flex w-full rounded-md border border-border bg-transparent px-3 py-1.5 text-sm text-foreground transition-colors outline-none placeholder:text-muted-foreground/30 focus-visible:border-primary focus-visible:ring-1 focus-visible:ring-ring/50 resize-y"
                  placeholder="What is the purpose of this skill?"
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <Input
                  label="Triggers (comma-separated)"
                  value={importTriggers}
                  onChange={(e) => setImportTriggers(e.target.value)}
                  placeholder="search the web, query search"
                />
              </div>
            </div>

            <div className="flex flex-col gap-1.5 min-h-[220px]">
              <Label>SKILL.md Markdown Body</Label>
              <textarea
                value={importBody}
                onChange={(e) => setImportBody(e.target.value)}
                className="flex-1 w-full rounded-md border border-border bg-[#1E1E2E] px-3 py-2 font-mono text-xs text-[#E5E7EB] transition-colors outline-none focus-visible:border-primary focus-visible:ring-1 focus-visible:ring-ring/50 resize-y"
                placeholder="# Instructions for using this skill\n\n1. First do X\n2. Next do Y"
                spellCheck={false}
              />
            </div>
          </div>

          {importError && (
            <p className="text-xs text-destructive text-left flex items-center gap-1">
              <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
              {importError}
            </p>
          )}

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setImportDialogOpen(false)}
              disabled={importSaving}
            >
              Cancel
            </Button>
            <Button onClick={handleCreateSkill} disabled={importSaving}>
              {importSaving ? (
                <>
                  <Loader2 className="mr-1 h-3 w-3 animate-spin" />
                  Creating...
                </>
              ) : (
                'Create Skill'
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
