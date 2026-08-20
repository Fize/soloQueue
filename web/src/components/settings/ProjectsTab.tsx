import { useState, useEffect, useCallback } from 'react'
import { listProjects, createProject, updateProject, deleteProject } from '@/lib/api'
import type { Project } from '@/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { FolderOpen, Plus, Pencil, Trash2, Loader2 } from 'lucide-react'
import { useTranslation } from '@/lib/i18n'

interface ProjectDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: () => void
  editProject?: Project | null
}

function ProjectDialog({ open, onOpenChange, onSave, editProject }: ProjectDialogProps) {
  const [name, setName] = useState('')
  const [path, setPath] = useState('')
  const [description, setDescription] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const { t } = useTranslation()

  const isEdit = !!editProject

  // Auto-derive slug from name; only user-editable for new projects
  const derivedId = name.trim().toLowerCase().replace(/\s+/g, '-')

  useEffect(() => {
    if (open) {
      if (editProject) {
        setName(editProject.name)
        setPath(editProject.path)
        setDescription(editProject.description || '')
      } else {
        setName('')
        setPath('')
        setDescription('')
      }
      setError(null)
    }
  }, [open, editProject])

  const handleSave = async () => {
    if (!name.trim()) {
      setError(t('projects.projectNameRequired'))
      return
    }
    if (!path.trim()) {
      setError(t('projects.workingDirRequired'))
      return
    }

    setSaving(true)
    setError(null)
    try {
      if (isEdit) {
        await updateProject(editProject!.id, {
          name: name.trim(),
          path: path.trim(),
          description: description.trim(),
        })
      } else {
        await createProject({
          id: derivedId,
          name: name.trim(),
          path: path.trim(),
          description: description.trim(),
        })
      }
      onSave()
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('projects.failedToSave'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm w-[95vw]">
        <DialogHeader>
          <DialogTitle>{isEdit ? t('projects.editProject') : t('projects.createProject')}</DialogTitle>
          <DialogDescription>
            {isEdit
              ? t('projects.updateProject', { name: editProject?.name })
              : t('projects.addProjectDesc')}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 my-2 text-left">
          <Input
            label={t('projects.projectNameLabel')}
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('projects.projectNamePlaceholder')}
          />

          <div className="flex flex-col gap-1.5">
            <Label>{t('projects.workingDirPath')}</Label>
            <div className="flex gap-1.5">
              <Input
                value={path}
                onChange={(e) => setPath(e.target.value)}
                placeholder={t('projects.workingDirPlaceholder')}
                className="flex-1"
              />
            </div>
          </div>

          <div className="flex flex-col gap-1.5">
            <Label>{t('projects.description')}</Label>
            <Textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={3}
              placeholder={t('projects.descPlaceholder')}
            />
          </div>
        </div>

        {error && <p className="text-xs text-destructive text-left">{error}</p>}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t('common.cancel')}
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {saving ? (
              <>
                <Loader2 className="mr-1 h-3 w-3 animate-spin" />
                {t('common.saving')}
              </>
            ) : isEdit ? (
              t('projects.saveChanges')
            ) : (
              t('projects.createProjectBtn')
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function ProjectsTab() {
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<Project | null>(null)
  const { t } = useTranslation()

  // Dialog state
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingProject, setEditingProject] = useState<Project | null>(null)

  const fetchProjectsList = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await listProjects()
      setProjects(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('projects.failedToLoad'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    fetchProjectsList()
  }, [fetchProjectsList])

  const handleCreateProject = () => {
    setEditingProject(null)
    setDialogOpen(true)
  }

  const handleEditProject = (p: Project) => {
    setEditingProject(p)
    setDialogOpen(true)
  }

  const handleDeleteProject = (p: Project) => {
    setDeleteTarget(p)
  }

  const confirmDeleteProject = async () => {
    if (!deleteTarget) return
    try {
      await deleteProject(deleteTarget.id)
      setDeleteTarget(null)
      await fetchProjectsList()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('projects.failedToDelete'))
      setDeleteTarget(null)
    }
  }

  if (loading) {
    return <div className="text-sm text-muted-foreground">{t('projects.loading')}</div>
  }

  return (
    <div className="space-y-6">
      {error && (
        <div className="rounded-md border border-destructive/50 bg-destructive/10 px-4 py-2 text-xs text-destructive">
          {error}
        </div>
      )}

      <div className="border rounded-lg bg-card shadow-sm">
        <div className="flex items-center justify-between px-5 py-4 border-b border-border">
          <div className="flex items-center gap-2">
            <FolderOpen className="h-4 w-4 text-foreground" />
            <h3 className="text-sm font-bold text-foreground">{t('projects.title')}</h3>
            <Badge variant="secondary" className="text-[10px]">
              {projects.length}
            </Badge>
          </div>
          <Button size="sm" onClick={handleCreateProject} className="gap-1">
            <Plus className="h-3.5 w-3.5" />
            {t('projects.addProject')}
          </Button>
        </div>

        {projects.length === 0 ? (
          <div className="px-5 py-8 text-center">
            <p className="text-sm text-muted-foreground">{t('projects.noProjectsYet')}</p>
          </div>
        ) : (
          <div className="divide-y divide-border">
            {projects.map((proj) => (
              <div
                key={proj.id}
                className="px-5 py-4 flex items-center justify-between gap-3 hover:bg-muted/10 transition-colors"
              >
                <div className="min-w-0 flex-1 text-left">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium text-foreground truncate">
                      {proj.name}
                    </span>
                    <Badge variant="outline" className="text-[10px] font-mono">
                      {proj.id}
                    </Badge>
                  </div>
                  <p className="text-xs text-muted-foreground font-mono truncate mt-1">
                    {proj.path}
                  </p>
                  {proj.description && (
                    <p className="text-xs text-muted-foreground mt-1.5 leading-normal">
                      {proj.description}
                    </p>
                  )}
                </div>

                <div className="flex items-center gap-1 shrink-0">
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => handleEditProject(proj)}
                    title={t('projects.editProjectTooltip')}
                  >
                    <Pencil className="h-3 w-3" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => handleDeleteProject(proj)}
                    title={t('projects.deleteProjectTooltip')}
                    className="text-destructive hover:text-destructive"
                  >
                    <Trash2 className="h-3 w-3" />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <ProjectDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onSave={fetchProjectsList}
        editProject={editingProject}
      />
      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
        title={t('projects.deleteProject')}
        message={t('projects.deleteConfirmMsg', { name: deleteTarget?.name ?? '' })}
        destructive
        onConfirm={confirmDeleteProject}
        confirmLabel={t('projects.deleteProject')}
      />
    </div>
  )
}
