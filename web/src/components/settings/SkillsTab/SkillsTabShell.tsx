import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ChevronDown,
  ChevronUp,
  FileText,
  Folder,
  Loader2,
  RefreshCw,
  Search,
  Sparkles,
} from 'lucide-react'
import { fetchSkillDetail, fetchSkillFiles, type SkillFileEntry } from '@/lib/api'
import type { SkillInfo } from '@/types'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { useTranslation } from '@/lib/i18n'
import { useToolsAndSkillsStore } from '@/stores/toolsAndSkillsStore'

function formatSize(bytes?: number) {
  if (bytes === undefined) return ''
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function fileName(path: string) {
  return path.slice(path.lastIndexOf('/') + 1)
}

export function SkillsTab() {
  const { t } = useTranslation()
  const skills = useToolsAndSkillsStore((state) => state.skills)
  const skillsLoading = useToolsAndSkillsStore((state) => state.skillsLoading)
  const fetchSkills = useToolsAndSkillsStore((state) => state.fetchSkills)
  const [query, setQuery] = useState('')
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [details, setDetails] = useState<Record<string, SkillInfo>>({})
  const [files, setFiles] = useState<Record<string, SkillFileEntry[]>>({})
  const [loadingId, setLoadingId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const requestGeneration = useRef(0)
  const expandedIdRef = useRef<string | null>(null)

  useEffect(() => {
    void fetchSkills()
  }, [fetchSkills])

  const filteredSkills = useMemo(() => {
    const normalized = query.trim().toLowerCase()
    return (skills?.skills ?? []).filter((skill) => {
      if (!normalized) return true
      return [skill.id, skill.name, skill.description, ...(skill.triggers ?? [])]
        .join(' ')
        .toLowerCase()
        .includes(normalized)
    })
  }, [query, skills])

  const loadSkill = async (id: string) => {
    const generation = ++requestGeneration.current
    setLoadingId(id)
    try {
      const [detail, fileList] = await Promise.all([fetchSkillDetail(id), fetchSkillFiles(id)])
      if (generation !== requestGeneration.current) return
      setDetails((current) => ({ ...current, [id]: detail }))
      setFiles((current) => ({ ...current, [id]: fileList.files }))
    } catch {
      if (generation === requestGeneration.current) {
        setError(t('skills.loadInstalledFailed'))
      }
    } finally {
      if (generation === requestGeneration.current) {
        setLoadingId(null)
      }
    }
  }

  const toggleSkill = async (id: string) => {
    if (expandedId === id) {
      requestGeneration.current += 1
      expandedIdRef.current = null
      setExpandedId(null)
      return
    }
    expandedIdRef.current = id
    setExpandedId(id)
    setError(null)
    if (details[id] && files[id]) return
    await loadSkill(id)
  }

  const refreshSkills = async () => {
    requestGeneration.current += 1
    setDetails({})
    setFiles({})
    setError(null)
    const currentExpandedId = expandedIdRef.current
    await fetchSkills()
    if (expandedIdRef.current !== currentExpandedId) return

    const refreshedSkills = useToolsAndSkillsStore.getState().skills
    if (
      currentExpandedId &&
      refreshedSkills?.skills.some((skill) => skill.id === currentExpandedId)
    ) {
      await loadSkill(currentExpandedId)
    } else if (currentExpandedId) {
      expandedIdRef.current = null
      setExpandedId(null)
      setLoadingId(null)
    }
  }

  return (
    <div className="space-y-5">
      <div className="flex items-center gap-2">
        <div className="relative max-w-md flex-1">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            className="h-9 pl-9"
            placeholder={t('skills.searchInstalled')}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => void refreshSkills()}
          disabled={skillsLoading}
        >
          <RefreshCw className={skillsLoading ? 'animate-spin' : ''} />
          {t('common.refresh')}
        </Button>
      </div>

      {error && <p className="text-xs text-destructive">{error}</p>}
      {skillsLoading && !skills && (
        <div className="flex items-center justify-center gap-2 py-12 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          {t('skills.loadingSkills')}
        </div>
      )}
      {!skillsLoading && filteredSkills.length === 0 && (
        <div className="rounded-lg border border-dashed border-border py-10 text-center text-sm text-muted-foreground">
          {query ? t('skills.noSearchMatch') : t('skills.noSkillsYet')}
        </div>
      )}

      <div className="space-y-3">
        {filteredSkills.map((skill) => {
          const detail = details[skill.id]
          const isExpanded = expandedId === skill.id
          return (
            <div key={skill.id} className="overflow-hidden rounded-lg border border-border bg-card">
              <button
                type="button"
                className="flex w-full items-start gap-3 p-4 text-left hover:bg-muted/30"
                onClick={() => void toggleSkill(skill.id)}
              >
                <Sparkles className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
                <span className="min-w-0 flex-1">
                  <span className="flex flex-wrap items-center gap-2">
                    <span className="text-sm font-semibold text-foreground">
                      {skill.name || skill.id}
                    </span>
                  </span>
                  <span className="mt-1 block text-xs text-muted-foreground">
                    {skill.description || skill.id}
                  </span>
                  <span className="mt-2 block font-mono text-[10px] text-muted-foreground/70">
                    {skill.id}
                  </span>
                </span>
                {isExpanded ? (
                  <ChevronUp className="h-4 w-4 text-muted-foreground" />
                ) : (
                  <ChevronDown className="h-4 w-4 text-muted-foreground" />
                )}
              </button>

              {isExpanded && (
                <div className="border-t border-border bg-muted/10 p-4">
                  {loadingId === skill.id && !detail ? (
                    <div className="flex items-center gap-2 py-6 text-xs text-muted-foreground">
                      <Loader2 className="h-4 w-4 animate-spin" />
                      {t('skills.loadingDetails')}
                    </div>
                  ) : detail ? (
                    <div className="space-y-4">
                      <div className="flex flex-wrap gap-1.5">
                        {(detail.triggers ?? []).map((trigger) => (
                          <Badge key={trigger} variant="outline" className="text-[10px]">
                            {trigger}
                          </Badge>
                        ))}
                        {(detail.required_env ?? []).map((env) => (
                          <Badge key={env} variant="warning" className="font-mono text-[10px]">
                            {env}
                          </Badge>
                        ))}
                      </div>
                      <MarkdownPreview
                        content={detail.body || t('skills.noInstructions')}
                        className="prose-sm max-w-none"
                      />
                      <div>
                        <h4 className="mb-2 flex items-center gap-2 text-xs font-semibold text-foreground">
                          <Folder className="h-3.5 w-3.5" />
                          {t('skills.skillDirectoryFiles')}
                        </h4>
                        <div className="space-y-1 rounded border border-border bg-card p-2">
                          {(files[skill.id] ?? []).map((file) => (
                            <div
                              key={file.path}
                              className="flex items-center gap-2 text-xs text-muted-foreground"
                              style={{
                                paddingLeft: `${Math.min(4, file.path.split('/').length - 1) * 12}px`,
                              }}
                            >
                              {file.kind === 'directory' ? (
                                <Folder className="h-3.5 w-3.5" />
                              ) : (
                                <FileText className="h-3.5 w-3.5" />
                              )}
                              <span className="flex-1 truncate">{fileName(file.path)}</span>
                              {file.size !== undefined && (
                                <span className="text-[10px]">{formatSize(file.size)}</span>
                              )}
                            </div>
                          ))}
                          {(files[skill.id] ?? []).length === 0 && (
                            <p className="text-xs text-muted-foreground">
                              {t('skills.noFilesFound')}
                            </p>
                          )}
                        </div>
                      </div>
                    </div>
                  ) : null}
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
