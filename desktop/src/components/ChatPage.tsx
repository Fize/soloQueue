import { useEffect, useRef, useState, useMemo, useCallback } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { useShallow } from "zustand/react/shallow";
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
  FileText,
} from "lucide-react";
import { useAgentStore } from "@/stores/agentStore";
import { useRuntimeStore } from "@/stores/runtimeStore";

import { cn } from "@/lib/utils";
import type { AgentInfo, Project, AgentResponse, SkillInfo } from "@/types";
import { useResizablePanes } from "@/hooks/useResizablePanes";
import { useInputBadges } from "@/hooks/useInputBadges";
import { SessionInspectorPanel } from "./chat/SessionInspectorPanel";
import type { PreviewCommentSnapshot } from "@/types/annotation";
import { 
  listL2Groups, listProjects, getTeams, getSkills, listAgents,
} from "@/lib/api";
import { ChatDesignPanel } from "@/components/ChatDesignPanel";

export function ChatPage() {
  const { sessionId } = useParams<{ sessionId: string }>();
  const navigate = useNavigate();
  // Fine-grained subscriptions. The previous useChatStore() form subscribed
  // to the entire state object and re-rendered on every chat_chunk. We now
  // split it: useShallow on maps whose top-level value reference does not
  // change on chunk updates (streamingSessions/historyHasMore/historyLoading/
  // sessions), and a plain messages selector that re-renders on chunk (this
  // is unavoidable since the assistant message is growing per token), but
  // the per-message children short-circuit this cost via React.memo on
  // ChatMessageView — old messages keep their reference and skip rendering.
  const activeSessionId = useChatStore((s) => s.activeSessionId)
  const messages = useChatStore((s) => s.messages)
  const streamingSessions = useChatStore(useShallow((s) => s.streamingSessions))
  const delegating = useChatStore((s) => s.delegating)
  const sessions = useChatStore(useShallow((s) => s.sessions))
  const historyHasMore = useChatStore(useShallow((s) => s.historyHasMore))
  const historyLoading = useChatStore(useShallow((s) => s.historyLoading))
  const loadMoreHistory = useChatStore((s) => s.loadMoreHistory)
  const setActiveSession = useChatStore((s) => s.setActiveSession)
  const loadHistory = useChatStore((s) => s.loadHistory)
  const createL2Session = useChatStore((s) => s.createL2Session)
  const deleteL2Session = useChatStore((s) => s.deleteL2Session)

  const { send, cancel } = useChatStream();
  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const userScrolledUp = useRef(false);
  const loadingMore = useRef(false);
  const streaming = activeSessionId ? !!streamingSessions[activeSessionId] : false;
  const connectionStatus = useRuntimeStore((s) => s.connectionStatus);

  // macOS Inspector state
  const [showInspector, setShowInspector] = useState(false);
  const [inspectorTab, setInspectorTab] = useState<"files" | "changes" | "plan">("files");

  const toggleInspector = (open: boolean) => {
    useRuntimeStore.getState().setInspectorPanelWidth(open ? panelWidth : 0);
    setShowInspector(open);
  };
  const sidebarCollapsed = useRuntimeStore((s) => s.sidebarCollapsed);
  const setSidebarCollapsed = useRuntimeStore((s) => s.setSidebarCollapsed);
  const isDesignMode = useRuntimeStore((s) => s.isDesignMode);
  const setDesignMode = useRuntimeStore((s) => s.setDesignMode);

  // Design context exposed by ChatDesignPanel (used in handleSend)
  const designContextRef = useRef<{ activeDesignFile?: string; hasDrawings: boolean }>({ activeDesignFile: undefined, hasDrawings: false });
  const [designSelectedTarget, setDesignSelectedTarget] = useState<PreviewCommentSnapshot | null>(null);

  const {
    panelWidth,
    isResizing,
    splitContainerRef,
    handleResizeStart,
    containerWidth,
  } = useResizablePanes(isDesignMode, activeSessionId);

  const MIN_AREA_WIDTH = 200;

  const prevIsDesignModeRef = useRef(isDesignMode);
  useEffect(() => {
    const wasDesignMode = prevIsDesignModeRef.current;
    prevIsDesignModeRef.current = isDesignMode;
    if (wasDesignMode && !isDesignMode) {
      setShowInspector(false);
      useRuntimeStore.getState().setInspectorPanelWidth(0);
    }
  }, [isDesignMode]);

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
          // Fallback: if team has no linked projects, assign all known projects
          if (!groupProjects[team.name] || groupProjects[team.name].length === 0) {
            groupProjects[team.name] = [...projs];
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

  // currentMessages for the active session. The previous useChatStore()
  // subscription made this whole component re-render on every chat_chunk;
  // the per-message React.memo on ChatMessageView now prevents the old
  // messages from re-rendering, so this re-render is cheap.
  const currentMessages = messages[activeSessionId || ""] || [];
  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const hasActiveSession = activeSession != null;
  const activeGroup = activeSession?.group ?? null;
  const activeProjectPath = activeSession?.project_path ?? null;
  const isL1Session = activeSessionId === "l1";

  // Safety check: redirect away from plan tab if plans list becomes empty
  useEffect(() => {
    if (inspectorTab === "plan" && (!activeSession?.plans || activeSession.plans.length === 0)) {
      setInspectorTab("files");
    }
  }, [activeSession?.plans, inspectorTab]);

  // Sync selectors from active session when session changes
  useEffect(() => {
    if (activeSession) {
      setSelectedGroup(activeSession.group || "");
      setSelectedProjectPath(activeSession.project_path || "");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeSessionId]);





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
      // eslint-disable-next-line react-hooks/set-state-in-effect
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

  const activeAgentInstanceId = isL1Session ? (groupAgents[0]?.instance_id || null) : (activeAgent?.instance_id || null);
  const activeAgentState = isL1Session ? groupAgents[0]?.state : activeAgent?.state;
  const stream = useAgentStream(activeAgentInstanceId);

  const prevAgentState = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (activeSessionId && activeAgentState) {
      const wasProcessing = prevAgentState.current === "processing";
      const isDoneProcessing = activeAgentState !== "processing";
      if (wasProcessing && isDoneProcessing) {
        loadHistory(activeSessionId);
      } else if (!prevAgentState.current) {
        loadHistory(activeSessionId);
      }
    }
    prevAgentState.current = activeAgentState;
  }, [activeSessionId, activeAgentState, loadHistory]);

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
      activeAgentState === "processing" &&
      !streaming &&
      streamChatSegments.length > 0
    ) {
      // Guard: if currentMessages already has a live assistant message with
      // content (from chat_chunk events via WebSocket), prefer it over the
      // agent stream virtual message. The agent stream is a secondary source
      // and may lag behind or be incomplete.
      const hasAssistantContent = currentMessages.some(
        (m) => m.role === "assistant" && m.segments.some((s) => s.type === "content")
      )
      if (hasAssistantContent) {
        return currentMessages
      }
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
    activeAgentState,
    streaming,
    streamChatSegments,
  ]);

  // Hide model and task-level badges when agent is not actively processing.
  // L2 derives the model role from the task level (L0→fast, L1→universal, …).
  const { modelName: inputModelName, taskLevel: inputTaskLevel } = useInputBadges(
    activeAgent,
    isAgentProcessing || streaming || delegating,
    (taskLevel, agent, lastModel) => {
      // agent.model_id is already the effective model set by the router after routing.
      // No need to re-map via role keys (which would incorrectly show "superior" etc.).
      return agent?.model_id || lastModel;
    },
  );

  const handleSend = async (
    text: string,
    files?: { name: string; path: string }[],
    group?: string,
    projectPath?: string,
    selectedElement?: any,
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

    const { activeDesignFile, hasDrawings } = designContextRef.current;

    await send(text, files, targetSessionId, selectedElement, activeDesignFile, hasDrawings);
    setDesignSelectedTarget(null);
  };


  const contentSum = finalMessages.reduce((acc, msg) => {
    let sum = 0;
    for (const seg of msg.segments) {
      if (
        seg.type === "content" ||
        seg.type === "thinking" ||
        seg.type === "error" ||
        seg.type === "compact"
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

  const handleUserInteraction = useCallback(() => {
    userScrolledUp.current = true;
  }, []);

  // Reset scroll state on session change
  useEffect(() => {
    userScrolledUp.current = false;
  }, [activeSessionId]);


  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    let prevScrollTop = el.scrollTop;
    // Cooldown timer: when history is prepended at the top, scrollHeight
    // grows which can push the user back into the "scrollTop < 50" trigger
    // zone a second time before they recover. 200ms of quiet after a load
    // suppresses the re-trigger.
    let cooldownTimer: ReturnType<typeof setTimeout> | null = null;
    const handleScroll = () => {
      const { scrollTop, scrollHeight, clientHeight } = el;
      const isNearBottom = scrollHeight - scrollTop - clientHeight < 100;
      if (isNearBottom) {
        userScrolledUp.current = false;
      } else {
        // If scrollTop decreased, the user scrolled up.
        if (scrollTop < prevScrollTop) {
          userScrolledUp.current = true;
        }
      }
      prevScrollTop = scrollTop;

      const hasMore = activeSessionId ? historyHasMore[activeSessionId] : false;
      const isLoading = activeSessionId
        ? historyLoading[activeSessionId]
        : false;

      if (
        scrollTop < 50 &&
        hasMore &&
        !isLoading &&
        !loadingMore.current &&
        cooldownTimer === null
      ) {
        loadingMore.current = true;
        const prevHeight = scrollHeight;
        loadMoreHistory(activeSessionId || "").then(() => {
          if (scrollRef.current) {
            const diff = scrollRef.current.scrollHeight - prevHeight;
            scrollRef.current.scrollTop = diff;
          }
          loadingMore.current = false;
          cooldownTimer = setTimeout(() => {
            cooldownTimer = null;
          }, 200);
        });
      }
    };
    el.addEventListener("scroll", handleScroll);
    return () => {
      el.removeEventListener("scroll", handleScroll);
      if (cooldownTimer !== null) clearTimeout(cooldownTimer);
    };
  }, [activeSessionId, historyHasMore, historyLoading, loadMoreHistory]);

  useEffect(() => {
    if (userScrolledUp.current) return;
    bottomRef.current?.scrollIntoView({
      behavior: "auto",
    });

    if (finalMessages.length > 0) {
      lastScrolledSessionId.current = activeSessionId;
    }
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
    selectedTarget: designSelectedTarget,
    onClearSelectedTarget: () => setDesignSelectedTarget(null),
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
              taskLevel={inputTaskLevel}
              modelName={inputModelName}
              processing={false}
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
                    {activeSession?.plans && activeSession.plans.length > 0 && (
                      <button
                        onClick={() => setInspectorTab("plan")}
                        className={cn(
                          "flex shrink-0 items-center gap-1.5 px-2.5 py-1.5 rounded-md text-xs font-medium transition-colors cursor-pointer",
                          inspectorTab === "plan"
                            ? "bg-primary/10 text-primary"
                            : "text-muted-foreground hover:text-foreground hover:bg-foreground/5",
                        )}
                      >
                        <FileText className="h-3.5 w-3.5" />
                        Plan
                      </button>
                    )}
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
                      taskLevel={inputTaskLevel}
                      modelName={inputModelName}
                      processing={isAgentProcessing || streaming || delegating}
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
                        onUserInteraction={handleUserInteraction}
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
                  taskLevel={inputTaskLevel}
                  modelName={inputModelName}
                  processing={isAgentProcessing || streaming || delegating}
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
                    <ChatDesignPanel
                      isDesignMode={isDesignMode}
                      onDesignModeToggle={(enabled) => setDesignMode(enabled)}
                      panelWidth={panelWidth}
                      onResizeStart={handleResizeStart}
                      selectedProjectPath={selectedProjectPath}
                      selectedGroup={selectedGroup}
                      onSelectedTargetChange={setDesignSelectedTarget}
                      onDesignContextChange={(ctx) => { designContextRef.current = ctx; }}
                    />
                  ) : (
                    <SessionInspectorPanel
                      activeSession={activeSession}
                      inspectorTab={inspectorTab}
                      panelWidth={panelWidth}
                      projectPathFallback={selectedProjectPath || undefined}
                    />
                  )}
                </div>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
