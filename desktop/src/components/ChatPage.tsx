import { useEffect, useRef, useState, useMemo, useCallback } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { ChatMessageView } from "@/components/ChatMessage";
import { ChatInput } from "@/components/ChatInput";
import { useChatStore } from "@/stores/chatStore";
import { useChatStream } from "@/hooks/useChatStream";
import { useAgentStream } from "@/hooks/useAgentStream";
import {
  PanelRight,
  Loader2,
  Activity,
  Bot,
  FolderOpen,
  Layers,
  Palette,
  X,
  Plus,
  ChevronDown,
  List,
} from "lucide-react";
import { useAgentStore } from "@/stores/agentStore";
import { useRuntimeStore } from "@/stores/runtimeStore";

import { cn } from "@/lib/utils";
import type { AgentInfo, Project, AgentResponse, SkillInfo, FileInfo } from "@/types";
import { SessionChangesPanel } from "@/components/SessionChangesPanel";
import { SessionFilePanel } from "@/components/SessionFilePanel";
import { 
  listL2Groups, listProjects, getTeams, getSkills, listAgents,
  listFiles, getFileUrl, getHealthInfo, saveFile
} from "@/lib/api";
import { DesignPreview } from "@/components/DesignPreview";
import type { ColoredStroke } from "@/components/ui/DrawOverlay";

const DESIGN_MIN_RIGHT_RATIO = 0.5;
const DESIGN_LEFT_MIN_WIDTH = 320;
const DESIGN_DEFAULT_LEFT_WIDTH = 420;
const DESIGN_DEFAULT_LEFT_WIDTH_SMALL = 380;
const RESIZE_HANDLE_WIDTH = 4;

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

export function ChatPage() {
  const { sessionId } = useParams<{ sessionId: string }>();
  const navigate = useNavigate();
  const {
    activeSessionId,
    messages,
    streaming,
    delegating,
    sessions,
    historyHasMore,
    historyLoading,
    loadMoreHistory,
    loadSessions,
    setActiveSession,
    loadHistory,
    createL2Session,
    deleteL2Session,
  } = useChatStore();
  const { send, cancel } = useChatStream();
  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const userScrolledUp = useRef(false);
  const loadingMore = useRef(false);
  const connectionStatus = useRuntimeStore((s) => s.connectionStatus);
  const isProgrammaticScrolling = useRef(false);

  // macOS Inspector state
  const [showInspector, setShowInspector] = useState(false);
  const [inspectorTab, setInspectorTab] = useState<"files" | "changes">(
    "files",
  );

  const toggleInspector = (open: boolean) => {
    useRuntimeStore.getState().setInspectorPanelWidth(open ? panelWidth : 0);
    setShowInspector(open);
  };
  const sidebarCollapsed = useRuntimeStore((s) => s.sidebarCollapsed);
  const setSidebarCollapsed = useRuntimeStore((s) => s.setSidebarCollapsed);
  const isDesignMode = useRuntimeStore((s) => s.isDesignMode);
  const setDesignMode = useRuntimeStore((s) => s.setDesignMode);
  const [designHtmlContent, setDesignHtmlContent] = useState<string | null>(null);
  const [designError, setDesignError] = useState<string | null>(null);
  const [designFiles, setDesignFiles] = useState<FileInfo[]>([]);

  // activeTab persistence: restore from localStorage on mount, save on change
  const ACTIVE_TAB_KEY = 'soloqueue_design_active_tab';
  const [activeTab, setActiveTabRaw] = useState<string>(() => {
    try {
      return localStorage.getItem(ACTIVE_TAB_KEY) || 'sketch';
    } catch {
      return 'sketch';
    }
  });
  const setActiveTab = useCallback((tab: string) => {
    setActiveTabRaw(tab);
    try {
      localStorage.setItem(ACTIVE_TAB_KEY, tab);
    } catch {}
  }, []);

  // designMode persistence: restore the click/draw/interact sub-mode separately from the global boolean flag.
  const DESIGN_SUBMODE_KEY = 'soloqueue_design_submode';
  const [designMode, setDesignModeState] = useState<'click' | 'draw' | 'interact'>(() => {
    try {
      const saved = localStorage.getItem(DESIGN_SUBMODE_KEY);
      if (saved === 'click' || saved === 'draw' || saved === 'interact') return saved;
    } catch {}
    return 'click';
  });
  // Persist designMode to localStorage
  useEffect(() => {
    try { localStorage.setItem(DESIGN_SUBMODE_KEY, designMode); } catch {}
  }, [designMode]);
  const [currentColor, setCurrentColor] = useState<string>("#ef4444");
  const [strokes, setStrokes] = useState<ColoredStroke[]>([]);

  // Ref to read latest activeTab inside effects without adding it to deps
  const activeTabRef = useRef(activeTab);
  useEffect(() => { activeTabRef.current = activeTab; }, [activeTab]);

  // Closed tabs tracking: persist to localStorage so refresh remembers which preview tabs were closed
  const CLOSED_TABS_KEY = 'soloqueue_design_closed_tabs';
  const [closedTabs, setClosedTabs] = useState<Set<string>>(() => {
    try {
      const raw = localStorage.getItem(CLOSED_TABS_KEY);
      if (raw) return new Set(JSON.parse(raw));
    } catch {}
    return new Set<string>();
  });
  const [showFileDropdown, setShowFileDropdown] = useState(false);
  const fileDropdownRef = useRef<HTMLDivElement>(null);

  // Persist closedTabs to localStorage
  useEffect(() => {
    try {
      localStorage.setItem(CLOSED_TABS_KEY, JSON.stringify([...closedTabs]));
    } catch {}
  }, [closedTabs]);

  // Close file dropdown when clicking outside — use 'click' (not mousedown)
  // to avoid fighting with button onClick handlers
  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (fileDropdownRef.current && !fileDropdownRef.current.contains(e.target as Node)) {
        setShowFileDropdown(false);
      }
    }
    document.addEventListener('click', handleClickOutside);
    return () => document.removeEventListener('click', handleClickOutside);
  }, []);

  const hasAutoSavedSketch = useRef(false);

  // Reset auto-save flag when leaving or re-entering the blank sketch tab
  useEffect(() => {
    if (activeTab === 'sketch') {
      hasAutoSavedSketch.current = false;
    }
  }, [activeTab]);

  const handleStrokesChange = async (newStrokes: ColoredStroke[]) => {
    setStrokes(newStrokes);
    if (activeTab && activeTab !== 'sketch') {
      try {
        if (activeTab.endsWith('.sketch')) {
          // Write directly as JSON
          await saveFile(activeTab, JSON.stringify(newStrokes));
        } else {
          // Write embedded inside HTML
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
      // Auto-save blank sketch: create a new .sketch file on first stroke
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
  };

  const handleCreateNewSketch = async () => {
    let designDir = activeSession?.design_dir;
    
    // Fallback logic for designDir
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
  };

  // Resizable inspector panel
  const MIN_AREA_WIDTH = 200;
  const [panelWidth, setPanelWidth] = useState(300);
  const [isResizing, setIsResizing] = useState(false);
  const splitContainerRef = useRef<HTMLDivElement>(null);
  const resizeDragRef = useRef<{ startX: number; startPanelWidth: number } | null>(null);
  const hasManuallyResized = useRef(false);
  const isDesignModeRef = useRef(isDesignMode);
  useEffect(() => { isDesignModeRef.current = isDesignMode; }, [isDesignMode]);

  // Guard: when exiting design mode, ensure showInspector is false and
  // inspectorPanelWidth is reset. This defensive reset handles any race
  // condition (e.g. resize observer callbacks or stale event handlers)
  // that might leave showInspector=true, which would erroneously display
  // the Files/Changes panel after exiting design mode.
  const prevIsDesignModeRef = useRef(isDesignMode);
  useEffect(() => {
    const wasDesignMode = prevIsDesignModeRef.current;
    prevIsDesignModeRef.current = isDesignMode;
    
    // When entering design mode, reset the manual resize flag
    if (!wasDesignMode && isDesignMode) {
      hasManuallyResized.current = false;
    }
    
    // Only act on the transition from design → normal (not on initial mount
    // or normal → design, so we don't interfere with design mode setup).
    if (wasDesignMode && !isDesignMode) {
      setShowInspector(false);
      useRuntimeStore.getState().setInspectorPanelWidth(0);
    }
  }, [isDesignMode]);

  const handleResizeStart = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    resizeDragRef.current = { startX: e.clientX, startPanelWidth: panelWidth };
    setIsResizing(true);
  }, [panelWidth]);

  useEffect(() => {
    if (!isResizing) return;
    const handleMouseMove = (e: MouseEvent) => {
      if (!splitContainerRef.current) return;
      const rect = splitContainerRef.current.getBoundingClientRect();
      const drag = resizeDragRef.current;
      if (!drag) return;

      if (isDesignModeRef.current) {
        hasManuallyResized.current = true;
      }

      const newWidth = drag.startPanelWidth - (e.clientX - drag.startX);
      let clamped: number;
      if (isDesignModeRef.current) {
        // Design mode: preview is at least half; keep room for the chat pane and resize handle.
        const minRight = Math.floor(rect.width * DESIGN_MIN_RIGHT_RATIO);
        const maxRight = Math.max(minRight, rect.width - DESIGN_LEFT_MIN_WIDTH - RESIZE_HANDLE_WIDTH);
        clamped = Math.max(minRight, Math.min(newWidth, maxRight));
      } else {
        const maxWidth = Math.floor(rect.width * 0.6);
        clamped = Math.max(
          MIN_AREA_WIDTH,
          Math.min(newWidth, rect.width - MIN_AREA_WIDTH, maxWidth),
        );
      }
      setPanelWidth(clamped);
      useRuntimeStore.getState().setInspectorPanelWidth(clamped);
      if (clamped !== newWidth) {
        resizeDragRef.current = { startX: e.clientX, startPanelWidth: clamped };
      }
    };
    const handleMouseUp = () => {
      resizeDragRef.current = null;
      setIsResizing(false);
    };
    document.addEventListener("mousemove", handleMouseMove);
    document.addEventListener("mouseup", handleMouseUp);
    return () => {
      document.removeEventListener("mousemove", handleMouseMove);
      document.removeEventListener("mouseup", handleMouseUp);
    };
  }, [isResizing]);

  // Track split container width for responsive content sizing
  const [containerWidth, setContainerWidth] = useState(0);
  useEffect(() => {
    const el = splitContainerRef.current;
    if (!el) {
      setContainerWidth(0);
      return;
    }
    setContainerWidth(el.getBoundingClientRect().width);
    const ro = new ResizeObserver(([entry]) => {
      setContainerWidth(entry.contentRect.width);
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, [activeSessionId, isDesignMode]);

  // Design mode defaults to a larger right preview pane, and automatically adjusts as the container resizes
  // (e.g. when the sidebar collapses/expands) to keep the left chat width at its default,
  // unless the user resizes it manually.
  useEffect(() => {
    if (!isDesignMode || containerWidth <= 0) {
      return;
    }
    const minRight = Math.floor(containerWidth * DESIGN_MIN_RIGHT_RATIO);
    const maxRight = Math.max(minRight, containerWidth - DESIGN_LEFT_MIN_WIDTH - RESIZE_HANDLE_WIDTH);
    
    if (hasManuallyResized.current) {
      setPanelWidth((current) => {
        const next = Math.max(minRight, Math.min(current, maxRight));
        useRuntimeStore.getState().setInspectorPanelWidth(next);
        return next;
      });
      return;
    }

    const defaultLeft = containerWidth >= 768 ? DESIGN_DEFAULT_LEFT_WIDTH : DESIGN_DEFAULT_LEFT_WIDTH_SMALL;
    const targetWidth = Math.min(
      Math.max(containerWidth - defaultLeft - RESIZE_HANDLE_WIDTH, minRight),
      maxRight,
    );
    setPanelWidth(targetWidth);
    useRuntimeStore.getState().setInspectorPanelWidth(targetWidth);
  }, [isDesignMode, containerWidth]);

  // L2 redesign states
  const [l2Groups, setL2Groups] = useState<string[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [teamProjectsMap, setTeamProjectsMap] = useState<
    Record<string, Project[]>
  >({});
  const [skills, setSkills] = useState<SkillInfo[]>([]);
  const [registeredAgents, setRegisteredAgents] = useState<AgentResponse[]>([]);

  const [selectedGroup, setSelectedGroup] = useState<string>("");
  const [selectedProjectPath, setSelectedProjectPath] = useState<string>("");

  // Load L2 groups, projects, teams
  useEffect(() => {
    let active = true;
    async function loadInitialData() {
      try {
        const [groupNames, projs, teamsData, skillsResp, agentsResp] = await Promise.all([
          listL2Groups(),
          listProjects(),
          getTeams().catch(() => ({ teams: [] })),
          getSkills().catch(() => ({ skills: [], total: 0 })),
          listAgents().catch(() => []),
        ]);

        if (!active) return;

        setL2Groups(groupNames);
        setProjects(projs);
        setSkills(skillsResp.skills || []);
        setRegisteredAgents(agentsResp);

        const projectMap = new Map(projs.map((p) => [p.id, p]));
        const groupProjects: Record<string, Project[]> = {};
        for (const team of (teamsData as any).teams || []) {
          if (team.projects && Array.isArray(team.projects)) {
            for (const pid of team.projects) {
              const proj = projectMap.get(pid);
              if (proj) {
                if (!groupProjects[team.name]) groupProjects[team.name] = [];
                groupProjects[team.name].push(proj);
              }
            }
          }
        }
        setTeamProjectsMap(groupProjects);
      } catch (err) {
        console.error("Failed to load welcome screen options:", err);
      }
    }
    loadInitialData();
    return () => {
      active = false;
    };
  }, []);

  const agentsData = useAgentStore((state) => state.agents);
  const teamsData = useAgentStore((state) => state.teams);
  const fetchLiveAgents = useAgentStore((state) => state.fetchLiveAgents);
  const fetchTeams = useAgentStore((state) => state.fetchTeams);

  useEffect(() => {
    fetchLiveAgents();
    fetchTeams();
  }, [fetchLiveAgents, fetchTeams]);

  useEffect(() => {
    loadSessions();
  }, [loadSessions]);

  useEffect(() => {
    if (sessionId && sessionId !== "l1") {
      if (sessionId !== activeSessionId) {
        setActiveSession(sessionId);
      }
    } else {
      // No session selected — show New Chat home (Welcome screen)
      if (activeSessionId) {
        setActiveSession("");
      }
    }
  }, [sessionId, activeSessionId, setActiveSession]);

  const currentMessages = messages[activeSessionId || ""] || [];
  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const hasActiveSession = activeSession != null;
  const activeGroup = activeSession?.group ?? null;
  const activeProjectPath = activeSession?.project_path ?? null;
  const isL1Session = activeSessionId === "l1";

  // Sync selectors from active session when session changes
  useEffect(() => {
    if (activeSession) {
      setSelectedGroup(activeSession.group || "");
      setSelectedProjectPath(activeSession.project_path || "");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeSessionId]);

  // Load design files listing — runs only when isDesignMode, activeSession or selectedProjectPath changes.
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
        
        // Fallback for older binaries or un-refreshed sessions
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
          if (!cancelled) setDesignFiles([]);
          return;
        }

        const list = await listFiles(designDir);
        if (cancelled) return;
        const filteredFiles = list.filter(f => !f.isDir && (f.ext === '.html' || f.ext === '.htm' || f.ext === '.sketch'));
        setDesignFiles(filteredFiles);
        
        // After loading files, validate current activeTab via ref (latest value).
        // If the restored tab no longer exists in the new file list, fallback.
        const currentTab = activeTabRef.current;
        const valid = currentTab === 'sketch' || filteredFiles.some(f => f.path === currentTab);
        if (!valid) {
          if (filteredFiles.length > 0) {
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
  }, [isDesignMode, activeSession, selectedProjectPath, selectedGroup]); // NOTE: activeTab is intentionally omitted

  // Load HTML for design preview
  useEffect(() => {
    if (!isDesignMode) {
      setDesignHtmlContent(null);
      return;
    }
    // Sketchpad: welcome screen — we don't load any HTML content for it
    if (activeTab === 'sketch') {
      setDesignHtmlContent(null);
      setStrokes([]);
      return;
    }

    let cancelled = false;
    async function fetchHtml() {
      try {
        if (activeTab.endsWith('.sketch')) {
          // Load sketch strokes directly from JSON
          const res = await fetch(getFileUrl(activeTab));
          if (cancelled) return;
          if (!res.ok) throw new Error('Failed to load sketch content.');
          const text = await res.text();
          if (cancelled) return;
          try {
            const parsedStrokes = JSON.parse(text);
            setStrokes(parsedStrokes);
          } catch {
            setStrokes([]);
          }
          // Render blank canvas HTML for sketch files
          const BLANK_HTML = `<!DOCTYPE html><html><head><meta charset="UTF-8"><style>body { margin:0; background:#fafafa; background-image: radial-gradient(circle, #e5e7eb 1.5px, transparent 1.5px); background-size: 24px 24px; height:100vh; width:100vw; overflow:hidden; }</style></head><body></body></html>`;
          setDesignHtmlContent(BLANK_HTML);
          setDesignError(null);
        } else {
          // Load HTML file content
          const res = await fetch(getFileUrl(activeTab));
          if (cancelled) return;
          if (!res.ok) throw new Error('Failed to load HTML content.');
          const text = await res.text();
          if (cancelled) return;
          setDesignHtmlContent(text);
          
          // Parse and restore strokes!
          const extracted = extractStrokesFromHtml(text);
          setStrokes(extracted);
          
          setDesignError(null);
        }
      } catch (err: any) {
        if (!cancelled) {
          setDesignHtmlContent(null);
          setDesignError("Failed to read HTML/sketch file.");
        }
      }
    }
    fetchHtml();
    return () => { cancelled = true; };
  }, [isDesignMode, activeTab, activeSession, selectedProjectPath]);

  // Dynamic message max-width: scales with main content area, capped at original 3xl (768px)
  const MESSAGE_MAX_W = 768;
  const messageMaxWidth = useMemo(() => {
    const panelVisible = (showInspector && activeSession) || isDesignMode;
    const mainContentWidth =
      containerWidth - (panelVisible ? panelWidth + 4 : 0); // 4px = handle width
    if (mainContentWidth <= 0) return MESSAGE_MAX_W;
    return Math.max(
      MIN_AREA_WIDTH - 32,
      Math.min(mainContentWidth * 0.85, MESSAGE_MAX_W),
    );
  }, [showInspector, isDesignMode, activeSession, containerWidth, panelWidth]);

  // Sync selected group and project path when active session data changes
  useEffect(() => {
    if (hasActiveSession) {
      setSelectedGroup(activeGroup || "");
      setSelectedProjectPath(activeProjectPath || "");
    } else if (l2Groups.length > 0) {
      setSelectedGroup(l2Groups[0]);
    }
  }, [hasActiveSession, activeGroup, activeProjectPath, l2Groups]);

  // Sync first project of selected group when selectedGroup changes
  useEffect(() => {
    if (selectedGroup) {
      const groupProjs = teamProjectsMap[selectedGroup] || [];
      setSelectedProjectPath((prevPath) => {
        const valid = groupProjs.some((p) => p.path === prevPath);
        if (!valid) {
          if (groupProjs.length > 0) return groupProjs[0].path;
          if (projects.length > 0) return projects[0].path;
        }
        return prevPath; // same ref → React bails, no re-render
      });
    }
  }, [selectedGroup, teamProjectsMap, projects]);

  const selectedProject = projects.find((p) => p.path === selectedProjectPath);

  const groupAgents = useMemo(() => {
    if (isL1Session) {
      let l1 = null;
      if (agentsData) {
        l1 = agentsData.agents.find((a) => a.id === "l1-agent");
      }
      const fallback: AgentInfo = {
        id: "main",
        instance_id: "",
        name: "L1 Agent",
        state: "stopped" as const,
        model_id: "Expert Model",
        provider_id: "",
        group: "L1",
        is_leader: true,
        task_level: "",
        error_count: 0,
        last_error: "",
        pending_delegations: 0,
        mailbox_high: 0,
        mailbox_normal: 0,
      };
      return [l1 || fallback];
    }

    if (!activeGroup) return [];

    const team = teamsData?.teams.find(
      (t) => t.name.toLowerCase() === activeGroup.toLowerCase(),
    );
    if (!team) {
      return agentsData
        ? agentsData.agents.filter(
            (a) => a.group?.toLowerCase() === activeGroup.toLowerCase(),
          )
        : [];
    }

    return team.agents.map((tmpl) => {
      const live = agentsData?.agents.find((a) => a.id === tmpl.id);
      const placeholder: AgentInfo = {
        id: tmpl.id,
        instance_id: "",
        name: tmpl.name,
        state: "stopped" as const,
        model_id: tmpl.model_id,
        provider_id: "",
        group: activeGroup,
        is_leader: tmpl.is_leader,
        task_level: "",
        error_count: 0,
        last_error: "",
        pending_delegations: 0,
        mailbox_high: 0,
        mailbox_normal: 0,
      };
      return live || placeholder;
    });
  }, [agentsData, teamsData, activeGroup, isL1Session]);

  const activeAgent = useMemo(() => {
    return groupAgents.find((a) => a.is_leader) || groupAgents[0] || null;
  }, [groupAgents]);

  const isAgentProcessing = activeAgent?.state === "processing";

  const agentDisplayName = useMemo(() => {
    if (isL1Session) return "L1 Agent";
    return activeSession?.agent_name || activeAgent?.name || "Assistant";
  }, [isL1Session, activeSession, activeAgent]);

  const l1Agent = isL1Session ? groupAgents[0] : null;
  const l1AgentState = l1Agent?.state;
  const l1AgentInstanceId = l1Agent?.instance_id || null;
  const stream = useAgentStream(l1AgentInstanceId);

  const prevL1AgentState = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (isL1Session && l1AgentState) {
      const wasProcessing = prevL1AgentState.current === "processing";
      const isDoneProcessing = l1AgentState !== "processing";
      if (wasProcessing && isDoneProcessing) {
        loadHistory("l1");
      } else if (!prevL1AgentState.current) {
        loadHistory("l1");
      }
    }
    prevL1AgentState.current = l1AgentState;
  }, [isL1Session, l1AgentState, loadHistory]);

  const streamChatSegments = useMemo(() => {
    if (!stream?.segments) return [];
    return stream.segments.map((seg) => {
      if (seg.type === "tool_call") {
        return {
          type: "tool_call" as const,
          callId: seg.call_id,
          name: seg.name,
          args: seg.args,
          result: seg.result || undefined,
          error: seg.error || undefined,
          durationMs: seg.duration_ms || undefined,
          done: seg.done,
        };
      }
      return seg;
    });
  }, [stream]);

  const finalMessages = useMemo(() => {
    if (
      isL1Session &&
      l1AgentState === "processing" &&
      !streaming &&
      streamChatSegments.length > 0
    ) {
      let base = currentMessages;
      while (base.length > 0 && base[base.length - 1].role === "assistant") {
        base = base.slice(0, -1);
      }
      const virtualMessage = {
        id: `msg-virtual-stream`,
        role: "assistant" as const,
        segments: streamChatSegments,
        timestamp: new Date().toISOString(),
      };
      return [...base, virtualMessage];
    }
    return currentMessages;
  }, [
    currentMessages,
    isL1Session,
    l1AgentState,
    streaming,
    streamChatSegments,
  ]);

  const handleSend = async (
    text: string,
    files?: { name: string; path: string }[],
    group?: string,
    projectPath?: string,
  ) => {
    let targetSessionId = activeSessionId || undefined;

    if (!isL1Session && group) {
      if (!activeSessionId) {
        // No session exists — auto-create one on first send
        const newId = await createL2Session(group, projectPath || "");
        if (newId) {
          targetSessionId = newId;
          navigate(`/chat/${newId}`);
        }
      } else if (currentMessages.length === 0 && activeSession) {
        // Session exists but no messages — recreate if context changed
        const currentProjPath = activeSession.project_path || "";
        const currentGroup = activeSession.group || "";

        if (group !== currentGroup || projectPath !== currentProjPath) {
          const newId = await createL2Session(group, projectPath || "");
          if (newId) {
            if (activeSessionId !== newId) {
              await deleteL2Session(activeSessionId);
            }
            targetSessionId = newId;
            navigate(`/chat/${newId}`);
          }
        }
      }
    }

    await send(text, files, targetSessionId);
  };

  const contentSum = finalMessages.reduce((acc, msg) => {
    let sum = 0;
    for (const seg of msg.segments) {
      if (
        seg.type === "content" ||
        seg.type === "thinking" ||
        seg.type === "error"
      ) {
        sum += (seg.text || "").length;
      } else if (seg.type === "tool_call") {
        sum +=
          (seg.name || "").length +
          (seg.args || "").length +
          (seg.result || "").length +
          (seg.error || "").length +
          (seg.done ? 1 : 0);
      } else if (seg.type === "delegation") {
        sum +=
          (seg.agentName || "").length +
          (seg.task || "").length +
          (seg.status || "").length +
          (seg.resultContent || "").length;
      } else if (seg.type === "tool_confirm") {
        sum +=
          (seg.name || "").length +
          (seg.prompt || "").length +
          (seg.resolved ? 1 : 0) +
          (seg.choice || "").length;
      }
    }
    return acc + sum + msg.segments.length;
  }, 0);

  const lastScrolledSessionId = useRef<string | null>(null);

  // Reset scroll state on session change
  useEffect(() => {
    userScrolledUp.current = false;
  }, [activeSessionId]);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const handleScroll = () => {
      const { scrollTop, scrollHeight, clientHeight } = el;
      const isNearBottom = scrollHeight - scrollTop - clientHeight < 100;
      if (isNearBottom) {
        userScrolledUp.current = false;
      } else if (!isProgrammaticScrolling.current) {
        userScrolledUp.current = true;
      }

      const hasMore = activeSessionId ? historyHasMore[activeSessionId] : false;
      const isLoading = activeSessionId
        ? historyLoading[activeSessionId]
        : false;

      if (scrollTop < 50 && hasMore && !isLoading && !loadingMore.current) {
        loadingMore.current = true;
        const prevHeight = scrollHeight;
        loadMoreHistory(activeSessionId || "").then(() => {
          if (scrollRef.current) {
            const diff = scrollRef.current.scrollHeight - prevHeight;
            scrollRef.current.scrollTop = diff;
          }
          loadingMore.current = false;
        });
      }
    };
    el.addEventListener("scroll", handleScroll);
    return () => el.removeEventListener("scroll", handleScroll);
  }, [activeSessionId, historyHasMore, historyLoading, loadMoreHistory]);

  useEffect(() => {
    if (userScrolledUp.current) return;
    // Use instant scroll (no animation) when not streaming.
    // During streaming, smooth scroll to follow live content.
    isProgrammaticScrolling.current = true;
    bottomRef.current?.scrollIntoView({
      behavior: streaming ? "smooth" : "auto",
    });
    const delay = streaming ? 800 : 100;
    const timer = setTimeout(() => {
      isProgrammaticScrolling.current = false;
    }, delay);

    if (finalMessages.length > 0) {
      lastScrolledSessionId.current = activeSessionId;
    }

    return () => clearTimeout(timer);
  }, [contentSum, streaming, activeSessionId, finalMessages]);

  const matchedRegAgent = useMemo(() => {
    if (!activeAgent || !registeredAgents) return null;
    return registeredAgents.find(
      (ra) => ra.id.toLowerCase() === activeAgent.id.toLowerCase()
    );
  }, [activeAgent, registeredAgents]);

  const filteredSkillNames = useMemo(() => {
    if (isL1Session || !activeSessionId) {
      // L1 session (Chief Secretary) or Welcome/Empty Screen (before selecting an L2 agent session)
      // "L1 默认有所有的 skill"
      return skills.map((s) => s.name);
    }
    if (matchedRegAgent) {
      const allowedIds = new Set(matchedRegAgent.skill_ids || []);
      return skills
        .filter((s) => allowedIds.has(s.id))
        .map((s) => s.name);
    }
    return [];
  }, [isL1Session, activeSessionId, matchedRegAgent, skills]);

  const sharedInputProps = {
    onSend: handleSend,
    onCancel: cancel,
    streaming,
    delegating,
    disabled: delegating || connectionStatus !== "connected",
    groups: l2Groups,
    projects,
    teamProjectsMap,
    selectedGroup,
    selectedProjectPath,
    onGroupChange: setSelectedGroup,
    onProjectChange: setSelectedProjectPath,
    skillNames: filteredSkillNames,
  };

  if (!activeSessionId && !isDesignMode) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center p-8 overflow-y-auto bg-background select-none h-full w-full">
        <div
          className="w-full flex flex-col items-center space-y-8"
          style={{ maxWidth: messageMaxWidth }}
        >
          <div className="text-center space-y-3">
            <div className="h-16 w-16 rounded-2xl bg-primary/10 border border-primary/20 flex items-center justify-center text-primary mx-auto mb-2 shadow-inner">
              <Bot className="h-8 w-8 animate-pulse" />
            </div>
            <h1 className="text-3xl font-extrabold tracking-tight text-foreground bg-gradient-to-r from-foreground to-foreground/75 bg-clip-text">
              Welcome to SoloQueue Workspace
            </h1>
            <p className="text-sm text-muted-foreground max-w-md mx-auto text-center">
              Select a team and project to start collaborative programming with
              a multi-agent system.
            </p>
          </div>

          {/* ChatInput with selectors — available immediately for composing first message */}
          <div className="w-full">
            <ChatInput
              {...sharedInputProps}
              activeSessionId={undefined}
              showL2Selectors={true}
              ctxwinUsed={0}
              ctxwinLimit={0}
              atRootDir={selectedProjectPath || ""}
              taskLevel={
                isAgentProcessing
                  ? activeAgent?.task_level || activeAgent?.last_level
                  : undefined
              }
              modelName={isAgentProcessing ? activeAgent?.model_id : undefined}
            />
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full w-full overflow-hidden bg-background">
      {/* Pane 3: Chat conversation bubble stream */}
      <div className="flex flex-1 flex-col overflow-hidden h-full bg-background relative">
        {/* Chat header — split into chat section + panel section when inspector is open */}
        <header
          className={cn(
            "flex h-12 items-center border-b border-border/30 select-none bg-card/20 shrink-0",
            sidebarCollapsed && "pl-[115px]",
            isDesignMode && "electron-drag"
          )}
        >
          {/* Left section: chat header area — fills remaining space */}
          <div
            className={cn(
              "flex items-center gap-3 px-6 h-full",
              showInspector
                ? "flex-1 justify-between"
                : "flex-1 justify-between",
            )}
          >
            <div className="flex items-center gap-2 min-w-0">
              <h1 className="text-xs font-bold text-foreground truncate font-mono">
                {activeSession?.name ||
                  (isL1Session ? "General Q&A (L1)" : `${activeGroup} Team`)}
              </h1>
              {connectionStatus === "reconnecting" && (
                <span className="flex items-center gap-1 text-[10px] text-amber-500 font-medium animate-pulse shrink-0">
                  <span className="h-1.5 w-1.5 rounded-full bg-amber-500 animate-ping absolute inline-flex h-1.5 w-1.5 rounded-full opacity-75" />
                  <span className="relative inline-flex rounded-full h-1.5 w-1.5 bg-amber-500" />
                  Connecting...
                </span>
              )}
              {connectionStatus === "disconnected" && (
                <span className="flex items-center gap-1 text-[10px] text-destructive font-medium animate-pulse shrink-0">
                  <span className="h-1.5 w-1.5 rounded-full bg-destructive" />
                  Disconnected
                </span>
              )}
            </div>
            <div className="flex items-center gap-2 electron-no-drag">
              {!showInspector && !isDesignMode && (
                <button
                  onClick={() => toggleInspector(true)}
                  className="p-1.5 rounded-md hover:bg-foreground/5 transition-all cursor-pointer text-muted-foreground"
                  title="Show Task Status Panel"
                >
                  <PanelRight className="h-4 w-4" />
                </button>
              )}
            </div>
          </div>

          {/* Right section: panel header area — aligned to inspector width */}
          {((showInspector && activeSession) || isDesignMode) && (
            <div
              className="shrink-0 flex items-center justify-between h-full border-l border-border/30 bg-card/20 px-3"
              style={{ width: panelWidth }}
            >
              {isDesignMode ? (
                <div className="flex items-center justify-between w-full">
                  <div className="flex items-center gap-2">
                    <Palette className="h-4 w-4 text-primary animate-pulse" />
                    <span className="text-xs font-bold text-foreground font-mono">DESIGN PREVIEW</span>
                  </div>
                  <button
                    onClick={() => {
                      setDesignMode(false);
                      setSidebarCollapsed(false);
                    }}
                    className="shrink-0 p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-foreground/5 transition-colors cursor-pointer"
                    title="Exit Design Mode"
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                </div>
              ) : (
                <>
                  <div className="flex items-center gap-1">
                    <button
                      onClick={() => setInspectorTab("files")}
                      className={cn(
                        "flex shrink-0 items-center gap-1.5 px-2.5 py-1.5 rounded-md text-xs font-medium transition-colors cursor-pointer",
                        inspectorTab === "files"
                          ? "bg-primary/10 text-primary"
                          : "text-muted-foreground hover:text-foreground hover:bg-foreground/5",
                      )}
                    >
                      <FolderOpen className="h-3.5 w-3.5" />
                      Files
                    </button>
                    <button
                      onClick={() => setInspectorTab("changes")}
                      className={cn(
                        "flex shrink-0 items-center gap-1.5 px-2.5 py-1.5 rounded-md text-xs font-medium transition-colors cursor-pointer",
                        inspectorTab === "changes"
                          ? "bg-primary/10 text-primary"
                          : "text-muted-foreground hover:text-foreground hover:bg-foreground/5",
                      )}
                    >
                      <Layers className="h-3.5 w-3.5" />
                      Changes
                    </button>
                  </div>
                  <button
                    onClick={() => toggleInspector(false)}
                    className="shrink-0 p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-foreground/5 transition-colors cursor-pointer"
                    title="Close Panel"
                  >
                    <PanelRight className="h-3.5 w-3.5" />
                  </button>
                </>
              )}
            </div>
          )}
        </header>

        {/* Outer container for chat content + inspector split layout */}
        <div
          ref={splitContainerRef}
          className={cn(
            "flex flex-1 min-h-0 overflow-hidden relative",
            isResizing && "select-none",
          )}
        >
          {/* Conversation stream */}
          <div className={cn(
            "flex flex-col min-w-0 h-full overflow-hidden bg-background",
            isResizing ? "transition-none" : "transition-all duration-300",
            isDesignMode ? "flex-1 min-w-[320px] border-r border-border/30" : "flex-1 min-w-0"
          )}>
            {finalMessages.length === 0 ? (
              <div className="flex-1 flex flex-col items-center justify-center p-6 overflow-y-auto bg-background">
                  <div
                    className={`w-full flex flex-col items-center space-y-8 ${isDesignMode ? 'px-2' : 'px-4'} select-none`}
                    style={{ maxWidth: messageMaxWidth }}
                  >
                  {/* Centered Heading */}
                  {!activeSessionId ? (
                    <div className="text-center space-y-3">
                      <div className="h-16 w-16 rounded-2xl bg-primary/10 border border-primary/20 flex items-center justify-center text-primary mx-auto mb-2 shadow-inner">
                        <Bot className="h-8 w-8 animate-pulse" />
                      </div>
                      <h1 className="text-3xl font-extrabold tracking-tight text-foreground bg-gradient-to-r from-foreground to-foreground/75 bg-clip-text">
                        Welcome to SoloQueue Workspace
                      </h1>
                      <p className="text-sm text-muted-foreground max-w-md mx-auto text-center font-normal">
                        Select a team and project to start collaborative programming with
                        a multi-agent system.
                      </p>
                    </div>
                  ) : (
                    <h1 className="text-3xl font-semibold text-foreground tracking-tight text-center">
                      {isL1Session
                        ? "What should we build with L1 Orchestrator?"
                        : `What should we build in ${selectedProject?.name || "soloQueue"}?`}
                    </h1>
                  )}

                  {/* Redesigned Input Card */}
                  <div className="w-full">
                    <ChatInput
                      {...sharedInputProps}
                      activeSessionId={activeSessionId || undefined}
                      showL2Selectors={!isL1Session}
                      ctxwinUsed={activeSession?.ctxwin_used ?? 0}
                      ctxwinLimit={activeSession?.ctxwin_limit ?? 0}
                      atRootDir={activeSession?.project_path || ""}
                      taskLevel={
                        isAgentProcessing
                          ? activeAgent?.task_level || activeAgent?.last_level
                          : undefined
                      }
                      modelName={isAgentProcessing ? activeAgent?.model_id : undefined}
                    />
                  </div>
                </div>
              </div>
            ) : (
              <>
                <div ref={scrollRef} className="flex-1 overflow-y-auto p-6">
                  <div
                    className={`mx-auto w-full space-y-6 ${isDesignMode ? 'px-2' : 'px-4'}`}
                    style={{ maxWidth: messageMaxWidth }}
                  >
                    {activeSessionId && historyLoading[activeSessionId] && (
                      <div className="flex items-center justify-center py-4">
                        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                        <span className="text-xs text-muted-foreground font-mono ml-2">
                          Loading history...
                        </span>
                      </div>
                    )}

                    {finalMessages.map((msg) => (
                      <ChatMessageView
                        key={msg.id}
                        message={msg}
                        agentName={agentDisplayName}
                      />
                    ))}

                    {delegating && (
                      <div className="flex items-center gap-2.5 text-xs text-muted-foreground bg-secondary/30 p-3 rounded-lg border border-border/25 font-mono animate-pulse">
                        <Activity className="h-3.5 w-3.5 text-primary animate-spin" />
                        <span>
                          Team is collaborating and delegating, please wait...
                        </span>
                      </div>
                    )}

                    <div ref={bottomRef} className="h-2" />
                  </div>
                </div>

                <ChatInput
                  {...sharedInputProps}
                  activeSessionId={activeSessionId || undefined}
                  showL2Selectors={!isL1Session}
                  readOnlySelectors={true}
                  ctxwinUsed={activeSession?.ctxwin_used ?? 0}
                  ctxwinLimit={activeSession?.ctxwin_limit ?? 0}
                  atRootDir={activeSession?.project_path || ""}
                  taskLevel={
                    isAgentProcessing
                      ? activeAgent?.task_level || activeAgent?.last_level
                      : undefined
                  }
                  modelName={isAgentProcessing ? activeAgent?.model_id : undefined}
                />
              </>
            )}
          </div>

          {/* Right Inspector panel or Design Preview */}
          {((showInspector && activeSession) || isDesignMode) && (
            <>
              {/* Resize handle */}
              <div
                onMouseDown={handleResizeStart}
                className={cn(
                  "w-1 shrink-0 cursor-col-resize hover:bg-primary/40 active:bg-primary/40 transition-colors",
                  isResizing && "bg-primary/40",
                )}
              />
              <div
                className={cn(
                  "border-l border-border/30 h-full overflow-hidden bg-card/5 flex flex-col shrink-0",
                  isResizing ? "transition-none" : "transition-all duration-300",
                )}
                style={{ width: panelWidth }}
              >
                {/* Panel content */}
                <div className="flex-1 min-h-0 overflow-hidden">
                  {isDesignMode ? (
                    <div className="flex flex-col h-full bg-white select-none relative">
                      {/* Tab Bar */}
                      <div className="flex items-center gap-1 bg-muted/20 border-b border-border/30 px-3 h-10 overflow-x-auto shrink-0 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
                        {/* Permanent Sketch Tab */}
                        <button
                          onClick={() => {
                            setActiveTab("sketch");
                            setStrokes([]);
                          }}
                          className={cn(
                            "flex items-center gap-1.5 px-3 h-8 rounded-t-lg text-xs font-semibold border-t border-x transition-colors cursor-pointer",
                            activeTab === "sketch"
                              ? "bg-white border-border/40 text-primary"
                              : "bg-transparent border-transparent text-muted-foreground hover:text-foreground hover:bg-muted/10"
                          )}
                        >
                          <Palette className="h-3 w-3" />
                          <span>Sketchpad</span>
                        </button>
                        
                        {/* HTML File Tabs — only show non-closed ones */}
                        {designFiles.filter((f) => !closedTabs.has(f.path)).map((file) => (
                          <div
                            key={file.path}
                            className={cn(
                              "flex items-center h-8 rounded-t-lg text-xs font-semibold border-t border-x transition-colors cursor-pointer max-w-[160px]",
                              activeTab === file.path
                                ? "bg-white border-border/40 text-primary"
                                : "bg-transparent border-transparent text-muted-foreground hover:text-foreground hover:bg-muted/10"
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
                              className="shrink-0 px-1 h-full text-muted-foreground/50 hover:text-destructive transition-colors"
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
                                "flex items-center gap-1 px-2 h-8 rounded-t-lg text-xs font-semibold border-t border-x transition-colors cursor-pointer",
                                showFileDropdown
                                  ? "bg-white border-border/40 text-primary"
                                  : "bg-transparent border-transparent text-muted-foreground hover:text-foreground hover:bg-muted/10"
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
                                    left: rect.right - 224, // 224 = w-56 (14rem = 224px)
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

                      {/* Design Canvas Preview Area */}
                      <div className="flex-1 min-h-0 overflow-hidden relative">
                        {activeTab === 'sketch' ? (
                          <div className="relative w-full h-full bg-[#fafafa] dark:bg-[#0b0c0e] overflow-hidden flex items-center justify-center select-none">
                            {/* Grid Background representing infinite canvas */}
                            <div className="absolute inset-0 opacity-[0.05] dark:opacity-[0.03] pointer-events-none" style={{ backgroundImage: 'radial-gradient(circle, currentColor 1.5px, transparent 1.5px)', backgroundSize: '24px 24px' }} />
                            
                            <div className="text-center z-10 p-8 max-w-sm rounded-3xl border border-border/40 bg-card/60 backdrop-blur-xl shadow-2xl mx-4 transition-all duration-300 hover:shadow-primary/5 hover:border-primary/20">
                              <div className="h-12 w-12 rounded-2xl bg-primary/10 flex items-center justify-center mx-auto mb-4 text-primary">
                                <Palette className="h-6 w-6" />
                              </div>
                              <h3 className="text-base font-bold text-foreground">Infinite Sketchpad</h3>
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
                            onSelectTarget={() => {
                              // Removed toast notification for selection
                            }}
                          />
                        ) : (
                          <div className="relative w-full h-full bg-[#fafafa] dark:bg-[#0b0c0e] overflow-hidden flex items-center justify-center select-none">
                            {/* Grid Background representing infinite canvas */}
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

                        {/* Floating Design Toolbar */}
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
                                  ? "bg-white shadow-sm text-primary"
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
                                  ? "bg-white shadow-sm text-primary"
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
                                  ? "bg-white shadow-sm text-primary"
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

                              {/* Custom Color Palette Picker */}
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
                  ) : inspectorTab === "files" ? (
                    activeSession?.project_path ? (
                      <SessionFilePanel
                        projectPath={activeSession.project_path}
                        panelWidth={panelWidth}
                      />
                    ) : (
                      <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
                        Current session not associated with a project
                      </div>
                    )
                  ) : activeSession ? (
                    <SessionChangesPanel sessionId={activeSession.id} />
                  ) : null}
                </div>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
