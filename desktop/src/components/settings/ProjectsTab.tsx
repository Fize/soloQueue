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

interface ProjectDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: () => void
  editProject?: Project | null
}

function ProjectDialog({ open, onOpenChange, onSave, editProject }: ProjectDialogProps) {
  const [id, setId] = useState('')
  const [name, setName] = useState('')
  const [path, setPath] = useState('')
  const [description, setDescription] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const isEdit = !!editProject

  useEffect(() => {
    if (open) {
      if (editProject) {
        setId(editProject.id)
        setName(editProject.name)
        setPath(editProject.path)
        setDescription(editProject.description || '')
      } else {
        setId('')
        setName('')
        setPath('')
        setDescription('')
      }
      setError(null)
    }
  }, [open, editProject])

  const handleSave = async () => {
    if (!id.trim() && !isEdit) {
      setError('Project ID is required')
      return
    }
    if (!name.trim()) {
      setError('Project name is required')
      return
    }
    if (!path.trim()) {
      setError('Working directory path is required')
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
          id: id.trim().toLowerCase().replace(/\s+/g, '-'),
          name: name.trim(),
          path: path.trim(),
          description: description.trim(),
        })
      }
      onSave()
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save project')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md w-[95vw]">
        <DialogHeader>
          <DialogTitle>{isEdit ? 'Edit Project' : 'Create Project'}</DialogTitle>
          <DialogDescription>
            {isEdit
              ? `Update project "${editProject?.name}" details`
              : 'Add a new project to organize your workspaces'}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 my-2 text-left">
          {!isEdit && (
            <Input
              label="Project ID / Slug"
              value={id}
              onChange={(e) => setId(e.target.value)}
              placeholder="e.g. my-web-app"
            />
          )}

          <Input
            label="Project Name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. My Web App"
          />

          <Input
            label="Working Directory Path"
            value={path}
            onChange={(e) => setPath(e.target.value)}
            placeholder="e.g. /Users/username/projects/my-web-app"
          />

          <div className="flex flex-col gap-1.5">
            <Label>Description</Label>
            <Textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={3}
              placeholder="Brief description of this project"
            />
          </div>
        </div>

        {error && <p className="text-xs text-destructive text-left">{error}</p>}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {saving ? (
              <>
                <Loader2 className="mr-1 h-3 w-3 animate-spin" />
                Saving...
              </>
            ) : isEdit ? (
              'Save Changes'
            ) : (
              'Create Project'
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
      setError(err instanceof Error ? err.message : 'Failed to load projects')
    } finally {
      setLoading(false)
    }
  }, [])

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
      setError(err instanceof Error ? err.message : 'Failed to delete project')
      setDeleteTarget(null)
    }
  }

  if (loading) {
    return <div className="text-sm text-muted-foreground">Loading projects...</div>
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
            <h3 className="text-sm font-bold text-foreground">Projects</h3>
            <Badge variant="secondary" className="text-[10px]">
              {projects.length}
            </Badge>
          </div>
          <Button size="sm" onClick={handleCreateProject} className="gap-1">
            <Plus className="h-3.5 w-3.5" />
            Add Project
          </Button>
        </div>

        {projects.length === 0 ? (
          <div className="px-5 py-8 text-center">
            <p className="text-sm text-muted-foreground">No projects created yet</p>
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
                    title="Edit project"
                  >
                    <Pencil className="h-3 w-3" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => handleDeleteProject(proj)}
                    title="Delete project"
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
        title="Delete Project"
        message={`Delete project "${deleteTarget?.name}"? This will not delete the files on disk.`}
        destructive
        onConfirm={confirmDeleteProject}
        confirmLabel="Delete Project"
      />
    </div>
  )
}
