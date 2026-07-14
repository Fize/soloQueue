import { useTranslation } from '@/lib/i18n'
import { useEffect, useRef, useState, useCallback } from "react";
import { useChatStore } from "@/stores/chatStore";
import {
  Palette,
  X,
  Plus,
  ChevronDown,
  List,
} from "lucide-react";
import { cn } from "@/lib/utils";
import type { FileInfo } from "@/types";
import type { PreviewCommentSnapshot } from "@/types/annotation";
import {
  listFiles, getFileUrl, getHealthInfo, saveFile
} from "@/lib/api";
import { DesignPreview } from "@/components/DesignPreview";
import type { ColoredStroke } from "@/components/ui/DrawOverlay";

// ─── localStorage keys ──────────────────────────────────────────────────────
const ACTIVE_TAB_KEY = 'soloqueue_design_active_tab';
const DESIGN_SUBMODE_KEY = 'soloqueue_design_submode';
const CLOSED_TABS_KEY = 'soloqueue_design_closed_tabs';

// ─── HTML stroke helpers ────────────────────────────────────────────────────

function updateStrokesInHtml(html: string, strokes: any[]): string {
  const marker = '<script id="sketch-data" type="application/json">';
  const markerEnd = '</script>';
  const startIndex = html.indexOf(marker);
  if (startIndex === -1) {
    const jsonStr = JSON.stringify(strokes);
    const scriptTag = `\n  ${marker}${jsonStr}${markerEnd}`;
    const headEnd = html.indexOf('</head>');
    if (headEnd !== -1) {
      return html.slice(0, headEnd) + scriptTag + html.slice(headEnd);
    }
    const bodyEnd = html.indexOf('</body>');
    if (bodyEnd !== -1) {
      return html.slice(0, bodyEnd) + scriptTag + html.slice(bodyEnd);
    }
    return html + scriptTag;
  }

  const contentStart = startIndex + marker.length;
  const endIndex = html.indexOf(markerEnd, contentStart);
  if (endIndex === -1) return html;

  return html.slice(0, contentStart) + JSON.stringify(strokes) + html.slice(endIndex);
}

function extractStrokesFromHtml(html: string): any[] {
  const marker = '<script id="sketch-data" type="application/json">';
  const markerEnd = '</script>';
  const startIndex = html.indexOf(marker);
  if (startIndex === -1) return [];
  const contentStart = startIndex + marker.length;
  const endIndex = html.indexOf(markerEnd, contentStart);
  if (endIndex === -1) return [];
  try {
    return JSON.parse(html.slice(contentStart, endIndex));
  } catch {
    return [];
  }
}

// ─── Props ──────────────────────────────────────────────────────────────────

export interface ChatDesignPanelProps {
  isDesignMode: boolean;
  onDesignModeToggle: (enabled: boolean) => void;
  panelWidth: number;
  onResizeStart: (e: React.MouseEvent) => void;
  selectedProjectPath: string;
  selectedGroup: string;
  /** Reports the currently selected element target back to the parent. */
  onSelectedTargetChange?: (target: PreviewCommentSnapshot | null) => void;
  /** Reports design-file context needed by the parent's send handler. */
  onDesignContextChange?: (ctx: { activeDesignFile?: string; hasDrawings: boolean }) => void;
}

// ─── Component ──────────────────────────────────────────────────────────────

export function ChatDesignPanel({
  isDesignMode,
  onResizeStart,
  selectedProjectPath,
  selectedGroup,
  onSelectedTargetChange,
  onDesignContextChange,
}: ChatDesignPanelProps) {
  const sessions = useChatStore((s) => s.sessions);
  const { t } = useTranslation();

  // ── Local state ───────────────────────────────────────────────────────────

  const [designHtmlContent, setDesignHtmlContent] = useState<string | null>(null);
  const [designError, setDesignError] = useState<string | null>(null);
  const [designFiles, setDesignFiles] = useState<FileInfo[]>([]);

  const [activeTab, setActiveTabRaw] = useState<string>(() => {
    try { return localStorage.getItem(ACTIVE_TAB_KEY) || 'sketch'; }
    catch { return 'sketch'; }
  });
  const setActiveTab = useCallback((tab: string) => {
    setActiveTabRaw(tab);
    try { localStorage.setItem(ACTIVE_TAB_KEY, tab); } catch {}
  }, []);

  const [designMode, setDesignModeState] = useState<'click' | 'draw' | 'interact'>(() => {
    try {
      const saved = localStorage.getItem(DESIGN_SUBMODE_KEY);
      if (saved === 'click' || saved === 'draw' || saved === 'interact') return saved;
    } catch {}
    return 'click';
  });
  useEffect(() => {
    try { localStorage.setItem(DESIGN_SUBMODE_KEY, designMode); } catch {}
  }, [designMode]);

  const [currentColor, setCurrentColor] = useState<string>("#ef4444");
  const [strokes, setStrokes] = useState<ColoredStroke[]>([]);
  const [selectedTarget, setSelectedTarget] = useState<PreviewCommentSnapshot | null>(null);

  // Ref to read latest activeTab inside effects without adding it to deps
  const activeTabRef = useRef(activeTab);
  useEffect(() => { activeTabRef.current = activeTab; }, [activeTab]);

  const [closedTabs, setClosedTabs] = useState<Set<string>>(() => {
    try {
      const raw = localStorage.getItem(CLOSED_TABS_KEY);
      if (raw) return new Set(JSON.parse(raw));
    } catch {}
    return new Set<string>();
  });
  const [showFileDropdown, setShowFileDropdown] = useState(false);
  const fileDropdownRef = useRef<HTMLDivElement>(null);

  const hasAutoSavedSketch = useRef(false);

  // ── Resolve active session ────────────────────────────────────────────────
  // We derive the session from the store so design_dir / project_path / group
  // are always in sync with the currently active session.
  const activeSession = sessions.find((s) => s.id === useChatStore.getState().activeSessionId) ?? null;

  // ── Persist closedTabs ────────────────────────────────────────────────────

  useEffect(() => {
    try { localStorage.setItem(CLOSED_TABS_KEY, JSON.stringify([...closedTabs])); } catch {}
  }, [closedTabs]);

  // ── Click outside file dropdown ───────────────────────────────────────────

  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (fileDropdownRef.current && !fileDropdownRef.current.contains(e.target as Node)) {
        setShowFileDropdown(false);
      }
    }
    document.addEventListener('click', handleClickOutside);
    return () => document.removeEventListener('click', handleClickOutside);
  }, []);

  // ── Reset auto-save flag when entering sketch tab ─────────────────────────

  useEffect(() => {
    if (activeTab === 'sketch') {
      hasAutoSavedSketch.current = false;
    }
  }, [activeTab]);

  // ── Report design context changes to parent ───────────────────────────────

  const hasDrawings = !!(activeTab && activeTab !== 'sketch' && strokes.length > 0);
  const activeDesignFile = activeTab && activeTab !== 'sketch' ? activeTab : undefined;
  useEffect(() => {
    onDesignContextChange?.({ activeDesignFile, hasDrawings });
  }, [activeDesignFile, hasDrawings, onDesignContextChange]);

  // ── Report selectedTarget to parent ───────────────────────────────────────

  useEffect(() => {
    onSelectedTargetChange?.(selectedTarget);
  }, [selectedTarget, onSelectedTargetChange]);

  // ── handleStrokesChange ───────────────────────────────────────────────────

  const handleStrokesChange = useCallback(async (newStrokes: ColoredStroke[]) => {
    setStrokes(newStrokes);
    if (activeTab && activeTab !== 'sketch') {
      try {
        if (activeTab.endsWith('.sketch')) {
          await saveFile(activeTab, JSON.stringify(newStrokes));
        } else {
          const res = await fetch(getFileUrl(activeTab));
          if (!res.ok) return;
          const text = await res.text();
          const updatedHtml = updateStrokesInHtml(text, newStrokes);
          await saveFile(activeTab, updatedHtml);
        }
      } catch (err) {
        console.error("Failed to save strokes to file:", err);
      }
    } else if (activeTab === 'sketch' && newStrokes.length > 0 && !hasAutoSavedSketch.current) {
      hasAutoSavedSketch.current = true;
      let designDir = activeSession?.design_dir;
      if (!designDir) {
        const projectPath = activeSession?.project_path || selectedProjectPath;
        const group = activeSession?.group || selectedGroup;
        if (projectPath) {
          designDir = `${projectPath}/.soloqueue/design`;
        } else if (group) {
          try {
            const health = await getHealthInfo();
            if (health.work_dir) {
              designDir = `${health.work_dir}/workspace/${group}/design`;
            }
          } catch {}
        }
      }
      if (designDir) {
        try {
          const index = designFiles.filter(f => f.name.startsWith('sketch_') && f.ext === '.sketch').length + 1;
          const filename = `sketch_${index}.sketch`;
          const fullPath = `${designDir}/${filename}`;
          await saveFile(fullPath, JSON.stringify(newStrokes));
          const list = await listFiles(designDir);
          const filteredFiles = list.filter(f => !f.isDir && (f.ext === '.html' || f.ext === '.htm' || f.ext === '.sketch'));
          setDesignFiles(filteredFiles);
          setActiveTab(fullPath);
        } catch (err) {
          console.error("Failed to auto-save blank sketch:", err);
          hasAutoSavedSketch.current = false;
        }
      }
    }
  }, [activeTab, activeSession, selectedProjectPath, selectedGroup, designFiles, setActiveTab]);

  // ── handleCreateNewSketch ─────────────────────────────────────────────────

  const handleCreateNewSketch = useCallback(async () => {
    let designDir = activeSession?.design_dir;
    if (!designDir) {
      const projectPath = activeSession?.project_path || selectedProjectPath;
      const group = activeSession?.group || selectedGroup;
      if (projectPath) {
        designDir = `${projectPath}/.soloqueue/design`;
      } else if (group) {
        const health = await getHealthInfo();
        if (health.work_dir) {
          designDir = `${health.work_dir}/workspace/${group}/design`;
        }
      }
    }
    if (!designDir) {
      console.error("No design directory available.");
      return;
    }
    const index = designFiles.filter(f => f.name.startsWith('sketch_') && f.ext === '.sketch').length + 1;
    const filename = `sketch_${index}.sketch`;
    const fullPath = `${designDir}/${filename}`;
    try {
      await saveFile(fullPath, "[]");
      const list = await listFiles(designDir);
      const filteredFiles = list.filter(f => !f.isDir && (f.ext === '.html' || f.ext === '.htm' || f.ext === '.sketch'));
      setDesignFiles(filteredFiles);
      setActiveTab(fullPath);
    } catch (err) {
      console.error("Failed to create new sketch file:", err);
    }
  }, [activeSession, selectedProjectPath, selectedGroup, designFiles, setActiveTab]);

  // ── Load design files listing ─────────────────────────────────────────────

  useEffect(() => {
    if (!isDesignMode) {
      setDesignFiles([]);
      return;
    }
    const session = activeSession;
    let cancelled = false;
    async function loadFiles() {
      try {
        let designDir = session?.design_dir;
        if (!designDir) {
          const projectPath = session?.project_path || selectedProjectPath;
          const group = session?.group || selectedGroup;
          if (projectPath) {
            designDir = `${projectPath}/.soloqueue/design`;
          } else if (group) {
            const health = await getHealthInfo();
            if (health.work_dir) {
              designDir = `${health.work_dir}/workspace/${group}/design`;
            }
          }
        }
        if (!designDir) {
          if (!cancelled) setDesignFiles([]);
          return;
        }
        const list = await listFiles(designDir);
        if (cancelled) return;
        const filteredFiles = list.filter(f => !f.isDir && (f.ext === '.html' || f.ext === '.htm' || f.ext === '.sketch'));
        setDesignFiles(filteredFiles);

        const currentTab = activeTabRef.current;
        const projectPath = session?.project_path || selectedProjectPath;
        let targetTab = currentTab;
        if (projectPath && currentTab === 'sketch' && filteredFiles.length > 0) {
          const firstHtml = filteredFiles.find(f => f.ext === '.html' || f.ext === '.htm');
          if (firstHtml) targetTab = firstHtml.path;
          else targetTab = filteredFiles[0].path;
        }
        const valid = targetTab === 'sketch' || filteredFiles.some(f => f.path === targetTab);
        if (!valid || targetTab !== currentTab) {
          if (filteredFiles.some(f => f.path === targetTab)) {
            setActiveTab(targetTab);
          } else if (filteredFiles.length > 0) {
            setActiveTab(filteredFiles[0].path);
          } else {
            setActiveTab('sketch');
          }
        }
      } catch {
        if (!cancelled) setDesignFiles([]);
      }
    }
    loadFiles();
    return () => { cancelled = true; };
    // NOTE: activeTab intentionally omitted (read via ref)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isDesignMode, activeSession, selectedProjectPath, selectedGroup]);

  // ── Load HTML for design preview ──────────────────────────────────────────

  useEffect(() => {
    if (!isDesignMode) {
      setDesignHtmlContent(null);
      return;
    }
    if (activeTab === 'sketch') {
      setDesignHtmlContent(null);
      setStrokes([]);
      return;
    }
    let cancelled = false;
    async function fetchHtml() {
      try {
        if (activeTab.endsWith('.sketch')) {
          const res = await fetch(getFileUrl(activeTab));
          if (cancelled) return;
          if (!res.ok) throw new Error('Failed to load sketch content.');
          const text = await res.text();
          if (cancelled) return;
          try { setStrokes(JSON.parse(text)); } catch { setStrokes([]); }
          const BLANK_HTML = `<!DOCTYPE html><html><head><meta charset="UTF-8"><style>body { margin:0; background:#fafafa; background-image: radial-gradient(circle, #e5e7eb 1.5px, transparent 1.5px); background-size: 24px 24px; height:100vh; width:100vw; overflow:hidden; }</style></head><body></body></html>`;
          setDesignHtmlContent(BLANK_HTML);
          setDesignError(null);
        } else {
          const res = await fetch(getFileUrl(activeTab));
          if (cancelled) return;
          if (!res.ok) throw new Error('Failed to load HTML content.');
          const text = await res.text();
          if (cancelled) return;
          setDesignHtmlContent(text);
          setStrokes(extractStrokesFromHtml(text));
          setDesignError(null);
        }
      } catch {
        if (!cancelled) {
          setDesignHtmlContent(null);
          setDesignError("Failed to read HTML/sketch file.");
        }
      }
    }
    fetchHtml();
    return () => { cancelled = true; };
  }, [isDesignMode, activeTab, activeSession, selectedProjectPath]);

  // ── Render nothing if not in design mode ──────────────────────────────────

  if (!isDesignMode) return null;

  // ── JSX ───────────────────────────────────────────────────────────────────

  return (
    <div className="flex flex-col h-full bg-background select-none relative">
      {/* ── Tab Bar ──────────────────────────────────────────────────────── */}
      <div className="flex items-center gap-1 bg-background border-b border-border px-3 h-10 overflow-x-auto shrink-0 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
        {/* Permanent Sketch Tab */}
        <button
          onClick={() => {
            setActiveTab("sketch");
            setStrokes([]);
          }}
          className={cn(
            "flex items-center gap-1.5 px-3 h-10 text-xs font-semibold border-b-2 transition-colors cursor-pointer -mb-[1px]",
            activeTab === "sketch"
              ? "border-primary text-primary"
              : "border-transparent text-muted-foreground hover:text-foreground hover:bg-muted/10"
          )}
        >
          <Palette className="h-3 w-3" />
          <span>{t('common.sketchpad')}</span>
        </button>

        {/* HTML File Tabs — only show non-closed ones */}
        {designFiles.filter((f) => !closedTabs.has(f.path)).map((file) => (
          <div
            key={file.path}
            className={cn(
              "flex items-center h-10 text-xs font-semibold border-b-2 transition-colors cursor-pointer max-w-[160px] -mb-[1px]",
              activeTab === file.path
                ? "border-primary text-primary"
                : "border-transparent text-muted-foreground hover:text-foreground hover:bg-muted/10"
            )}
          >
            <button
              onClick={() => {
                setActiveTab(file.path);
                setStrokes([]);
              }}
              className="flex items-center gap-1.5 px-2 h-full min-w-0"
              title={file.name}
            >
              <span className="shrink-0">{file.ext === '.sketch' ? "🎨" : "🌐"}</span>
              <span className="truncate">{file.name}</span>
            </button>
            <button
              onClick={(e) => {
                e.stopPropagation();
                const nextClosed = new Set([...closedTabs, file.path]);
                setClosedTabs(nextClosed);
                if (activeTab === file.path) {
                  const remaining = designFiles.filter(
                    (f) => f.path !== file.path && !nextClosed.has(f.path)
                  );
                  if (remaining.length > 0) {
                    setActiveTab(remaining[0].path);
                  } else {
                    setActiveTab('sketch');
                  }
                  setStrokes([]);
                }
              }}
              className="shrink-0 pr-2 pl-1 h-full text-muted-foreground/50 hover:text-destructive transition-colors flex items-center justify-center"
              title="Close preview tab"
            >
              <X className="h-3 w-3" />
            </button>
          </div>
        ))}

        {/* All files dropdown — reopen closed tabs */}
        {designFiles.length > 0 && (
          <div className="relative shrink-0 ml-auto" ref={fileDropdownRef}>
            <button
              onClick={(e) => {
                e.stopPropagation();
                setShowFileDropdown(!showFileDropdown);
              }}
              className={cn(
                "flex items-center gap-1 px-2 h-10 text-xs font-semibold border-b-2 transition-colors cursor-pointer -mb-[1px]",
                showFileDropdown
                  ? "border-primary text-primary"
                  : "border-transparent text-muted-foreground hover:text-foreground hover:bg-muted/10"
              )}
              title="All design files"
            >
              <List className="h-3 w-3" />
              <ChevronDown className={`h-3 w-3 transition-transform ${showFileDropdown ? 'rotate-180' : ''}`} />
            </button>
            {showFileDropdown && (
              <div
                className="fixed z-[100] mt-1 w-56 rounded-xl border border-border/40 bg-background shadow-xl overflow-hidden"
                style={(() => {
                  const rect = fileDropdownRef.current?.getBoundingClientRect();
                  if (!rect) return {};
                  return {
                    top: rect.bottom + 4,
                    left: rect.right - 224,
                  };
                })()}
              >
                <div className="max-h-64 overflow-y-auto py-1">
                  {designFiles.map((file) => {
                    const isClosed = closedTabs.has(file.path);
                    const isActive = activeTab === file.path;
                    return (
                      <button
                        key={file.path}
                        onClick={(e) => {
                          e.stopPropagation();
                          if (isClosed) {
                            setClosedTabs((prev) => {
                              const next = new Set(prev);
                              next.delete(file.path);
                              return next;
                            });
                          }
                          setActiveTab(file.path);
                          setStrokes([]);
                          setShowFileDropdown(false);
                        }}
                        className={cn(
                          "flex items-center gap-2 w-full px-3 py-2 text-left text-xs transition-colors",
                          isActive
                            ? "bg-primary/10 text-primary font-semibold"
                            : "text-foreground hover:bg-muted/50"
                        )}
                      >
                        <span className="shrink-0">{file.ext === '.sketch' ? "🎨" : "🌐"}</span>
                        <span className="truncate flex-1">{file.name}</span>
                        {isClosed && (
                          <span className="shrink-0 text-[10px] text-muted-foreground/60 bg-muted px-1.5 py-0.5 rounded">
                            closed
                          </span>
                        )}
                      </button>
                    );
                  })}
                </div>
              </div>
            )}
          </div>
        )}
      </div>

      {/* ── Design Canvas Preview Area ────────────────────────────────────── */}
      <div className="flex-1 min-h-0 overflow-hidden relative">
        {activeTab === 'sketch' ? (
          <div className="relative w-full h-full bg-background overflow-hidden flex items-center justify-center select-none">
            <div className="absolute inset-0 opacity-[0.05] dark:opacity-[0.03] pointer-events-none" style={{ backgroundImage: 'radial-gradient(circle, currentColor 1.5px, transparent 1.5px)', backgroundSize: '24px 24px' }} />
            <div className="text-center z-10 p-8 max-w-sm rounded-3xl border border-border/40 bg-card/60 backdrop-blur-xl shadow-2xl mx-4 transition-all duration-300 hover:shadow-primary/5 hover:border-primary/20">
              <div className="h-12 w-12 rounded-2xl bg-primary/10 flex items-center justify-center mx-auto mb-4 text-primary">
                <Palette className="h-6 w-6" />
              </div>
              <h3 className="text-base font-bold text-foreground">{t('common.sketchpad')}</h3>
              <p className="text-xs text-muted-foreground mt-2 mb-6 leading-relaxed">
                Create a blank canvas to sketch your ideas, draw wireframes, or annotate layouts.
              </p>
              <button
                onClick={handleCreateNewSketch}
                className="w-full py-2.5 px-4 bg-primary text-primary-foreground hover:bg-primary/95 font-semibold text-xs rounded-xl shadow-lg shadow-primary/10 transition-all flex items-center justify-center gap-2 hover:scale-[1.02] active:scale-[0.98] cursor-pointer"
              >
                <Plus className="h-4 w-4" />
                <span>New Sketch</span>
              </button>
            </div>
          </div>
        ) : designHtmlContent ? (
          <DesignPreview
            key={activeTab}
            htmlContent={designHtmlContent}
            mode={designMode}
            strokes={strokes}
            currentColor={currentColor}
            onStrokesChange={(s) => handleStrokesChange(s)}
            onSelectTarget={(t) => {
              setSelectedTarget(t);
            }}
            onResizeStart={onResizeStart}
          />
        ) : (
          <div className="relative w-full h-full bg-background overflow-hidden flex items-center justify-center select-none">
            <div className="absolute inset-0 opacity-[0.05] dark:opacity-[0.03] pointer-events-none" style={{ backgroundImage: 'radial-gradient(circle, currentColor 1.5px, transparent 1.5px)', backgroundSize: '24px 24px' }} />
            <div className="text-center z-10 p-6 max-w-sm rounded-2xl border border-border/40 bg-card/60 backdrop-blur-xl shadow-xl mx-4">
              <Palette className="h-8 w-8 text-primary mx-auto mb-3 animate-pulse" />
              <h3 className="text-sm font-bold text-foreground">Infinite Design Canvas</h3>
              <p className="text-[11px] text-muted-foreground mt-2 leading-relaxed">
                {designError || "No active HTML page found. Start chatting to generate UI code, then select and annotate it here."}
              </p>
            </div>
          </div>
        )}

        {/* ── Floating Design Toolbar ─────────────────────────────────────── */}
        <div
          className="absolute bottom-4 left-1/2 -translate-x-1/2 flex items-center gap-3 bg-background/80 backdrop-blur-xl border border-border/40 p-2.5 rounded-full shadow-2xl z-[60]"
          onClick={(e) => e.stopPropagation()}
          onMouseDown={(e) => e.stopPropagation()}
          onPointerDown={(e) => e.stopPropagation()}
          onPointerUp={(e) => e.stopPropagation()}
        >
          {/* Mode Selectors */}
          <div className="flex items-center gap-1 bg-muted/40 p-0.5 rounded-full border border-border/20">
            <button
              onClick={() => setDesignModeState('interact')}
              className={cn(
                "p-1.5 rounded-full cursor-pointer transition-all",
                designMode === 'interact'
                  ? "bg-background shadow-sm text-primary"
                  : "text-muted-foreground hover:text-foreground"
              )}
              title="Browse (Normal interaction)"
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="h-3.5 w-3.5">
                <rect x="6" y="3" width="12" height="18" rx="6"/>
                <line x1="12" y1="7" x2="12" y2="11"/>
              </svg>
            </button>
            <button
              onClick={() => setDesignModeState('click')}
              className={cn(
                "p-1.5 rounded-full cursor-pointer transition-all",
                designMode === 'click'
                  ? "bg-background shadow-sm text-primary"
                  : "text-muted-foreground hover:text-foreground"
              )}
              title="Pointer (Select Element)"
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" className="h-3.5 w-3.5"><path d="m4 4 7.07 17 2.51-7.39L21 11.07z"/></svg>
            </button>
            <button
              onClick={() => setDesignModeState('draw')}
              className={cn(
                "p-1.5 rounded-full cursor-pointer transition-all",
                designMode === 'draw'
                  ? "bg-background shadow-sm text-primary"
                  : "text-muted-foreground hover:text-foreground"
              )}
              title="Pen (Draw annotation)"
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" className="h-3.5 w-3.5"><path d="M12 20h9"/><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z"/></svg>
            </button>
          </div>

          {/* Color Picker */}
          {designMode === 'draw' && (
            <div className="flex items-center gap-1.5 border-l border-border/40 pl-2.5">
              {[
                { value: '#ef4444', label: 'Red' },
                { value: '#3b82f6', label: 'Blue' },
                { value: '#10b981', label: 'Green' },
                { value: '#eab308', label: 'Yellow' },
                { value: '#8b5cf6', label: 'Purple' }
              ].map((c) => (
                <button
                  key={c.value}
                  onClick={() => setCurrentColor(c.value)}
                  className={cn(
                    "h-4.5 w-4.5 rounded-full border cursor-pointer transition-all hover:scale-110",
                    currentColor === c.value
                      ? "border-foreground ring-1 ring-offset-1 ring-foreground"
                      : "border-transparent"
                  )}
                  style={{ backgroundColor: c.value }}
                  title={c.label}
                />
              ))}
              <label
                className={cn(
                  "h-4.5 w-4.5 rounded-full border border-border/20 cursor-pointer transition-all hover:scale-110 relative flex items-center justify-center bg-[conic-gradient(from_0deg,#ff0000,#ffff00,#00ff00,#00ffff,#0000ff,#ff00ff,#ff0000)]",
                  !['#ef4444', '#3b82f6', '#10b981', '#eab308', '#8b5cf6'].includes(currentColor)
                    ? "border-foreground ring-1 ring-offset-1 ring-foreground"
                    : "border-transparent"
                )}
                title="Custom Color Palette"
              >
                <input
                  type="color"
                  value={currentColor.startsWith('#') && currentColor.length === 7 ? currentColor : '#ef4444'}
                  onChange={(e) => setCurrentColor(e.target.value)}
                  className="absolute inset-0 opacity-0 w-full h-full cursor-pointer"
                />
              </label>
            </div>
          )}

          {/* Action Tools */}
          <div className="flex items-center gap-1 border-l border-border/40 pl-2.5">
            <button
              onClick={() => handleStrokesChange(strokes.slice(0, -1))}
              disabled={strokes.length === 0}
              className="p-1.5 rounded-full text-muted-foreground hover:text-foreground hover:bg-muted disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer"
              title="Undo last mark"
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" className="h-3.5 w-3.5"><path d="M3 7v6h6"/><path d="M21 17a9 9 0 0 0-9-9 9 9 0 0 0-6 2.3L3 13"/></svg>
            </button>
            <button
              onClick={() => handleStrokesChange([])}
              disabled={strokes.length === 0}
              className="p-1.5 rounded-full text-muted-foreground hover:text-destructive hover:bg-destructive/10 disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer"
              title="Clear all marks"
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" className="h-3.5 w-3.5"><path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/></svg>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
