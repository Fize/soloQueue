import { useEffect, useRef, useCallback, useMemo, useState } from "react";
import { ChatMessageView } from "@/components/ChatMessage";
import { ChatInput } from "@/components/ChatInput";
import { AgentWorkingIndicator } from "@/components/chat/AgentWorkingIndicator";
import { Sparkles, Loader2 } from "lucide-react";
import { useChatStore } from "@/stores/chatStore";
import { useChatStream } from "@/hooks/useChatStream";
import { useAgentStream } from "@/hooks/useAgentStream";
import { useAgentStore } from "@/stores/agentStore";
import { useRuntimeStore } from "@/stores/runtimeStore";
import { cn } from "@/lib/utils";
import { getSkills } from "@/lib/api";
import { wsManager } from "@/lib/websocket";
import type { SkillInfo, ChatSegment } from "@/types";
import { StickyToolConfirmPanel } from "@/components/chat";
import { recoverInFlightMessages } from "@/components/chat/recoverInFlightMessages";

export function AssistantPage() {
  const {
    activeSessionId,
    messages,
    streamingSessions,
    systemCommandSessions,
    delegatingSessions,
    routeSessions,
    historyHasMore,
    historyLoading,
    loadMoreHistory,
    setActiveSession,
    loadHistory,
  } = useChatStore();

  const delegating = activeSessionId ? (delegatingSessions[activeSessionId] ?? false) : false;
  const isSystemCommandRunning = activeSessionId
    ? (systemCommandSessions[activeSessionId] ?? false)
    : false;

  const { send, cancel } = useChatStream();
  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const loadingMoreRef = useRef(false);
  const userScrolledUpRef = useRef(false);

  const handleUserInteraction = useCallback(() => {
    userScrolledUpRef.current = true;
  }, []);

  const [skills, setSkills] = useState<SkillInfo[]>([]);

  useEffect(() => {
    let active = true;
    async function loadData() {
      try {
        const skillsResp = await getSkills().catch(() => ({ skills: [], total: 0 }));
        if (!active) return;
        setSkills(skillsResp.skills || []);
      } catch (err) {
        console.error("Failed to load skills", err);
      }
    }
    loadData();
    return () => {
      active = false;
    };
  }, []);

  // Set active session to L1 on mount
  useEffect(() => {
    setActiveSession("l1");
  }, [setActiveSession]);

  // Agent name & state from stores
  const agentsData = useAgentStore((state) => state.agents);
  const runtimeStatus = useRuntimeStore((state) => state.status);
  const connectionStatus = useRuntimeStore((state) => state.connectionStatus);
  const streaming = activeSessionId ? !!streamingSessions[activeSessionId] : false;
  const sidebarCollapsed = useRuntimeStore((state) => state.sidebarCollapsed);

  const agentName = useMemo(() => {
    const l1 = agentsData?.agents.find((a) => a.id === "l1-agent");
    return l1?.name || "L1 Agent";
  }, [agentsData]);

  const isL1Session = activeSessionId === "l1";
  const l1Agent = useMemo(() => {
    return agentsData?.agents.find((a) => a.id === "l1-agent") || null;
  }, [agentsData]);
  const l1AgentState = l1Agent?.state;
  const l1AgentInstanceId = l1Agent?.instance_id || null;
  const stream = useAgentStream(l1AgentInstanceId);

  const filteredSkillNames = useMemo(() => {
    return skills.map((s) => s.name);
  }, [skills]);

  // Context window tokens
  const l1Runtime = runtimeStatus?.sessions?.l1;
  const ctxwinUsed = l1Runtime?.ctxwin_used ?? 0;
  const ctxwinLimit = l1Runtime?.ctxwin_limit ?? 0;

  // ── Sync history upon agent state transitions (start/end processing) ──
  const prevL1AgentState = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (isL1Session && l1AgentState) {
      const wasProcessing = prevL1AgentState.current === "processing";
      const isProcessing = l1AgentState === "processing";
      if (
        prevL1AgentState.current !== undefined &&
        wasProcessing !== isProcessing
      ) {
        loadHistory("l1");
      } else if (prevL1AgentState.current === undefined) {
        loadHistory("l1");
      }
    }
    prevL1AgentState.current = l1AgentState;
  }, [isL1Session, l1AgentState, loadHistory]);

  // ── Load more history when scrolling up ───────────────────────────────────
  const hasMore = historyHasMore["l1"] ?? false;
  const isLoadingMore = historyLoading["l1"] ?? false;

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    let prevScrollTop = el.scrollTop;
    const handleScroll = () => {
      const { scrollTop, scrollHeight, clientHeight } = el;
      const isNearBottom = scrollHeight - scrollTop - clientHeight < 100;
      if (isNearBottom) {
        userScrolledUpRef.current = false;
      } else {
        // If scrollTop decreased, the user scrolled up.
        if (scrollTop < prevScrollTop) {
          userScrolledUpRef.current = true;
        }
      }
      prevScrollTop = scrollTop;

      if (
        scrollTop < 50 &&
        hasMore &&
        !isLoadingMore &&
        !loadingMoreRef.current
      ) {
        loadingMoreRef.current = true;
        const prevHeight = scrollHeight;
        loadMoreHistory("l1").then(() => {
          if (scrollRef.current) {
            const diff = scrollRef.current.scrollHeight - prevHeight;
            scrollRef.current.scrollTop = diff;
          }
          loadingMoreRef.current = false;
        });
      }
    };
    el.addEventListener("scroll", handleScroll);
    return () => el.removeEventListener("scroll", handleScroll);
  }, [hasMore, isLoadingMore, loadMoreHistory]);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const ro = new ResizeObserver(() => {
      if (!userScrolledUpRef.current) {
        el.scrollTop = el.scrollHeight;
      }
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // Auto-scroll to bottom
  const currentMessages = messages["l1"] || [];
  const contentSum = currentMessages.reduce((acc, msg) => {
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

  useEffect(() => {
    if (userScrolledUpRef.current) return;
    bottomRef.current?.scrollIntoView({
      behavior: "auto",
    });
  }, [contentSum, streaming]);

  // ── Send & Cancel ─────────────────────────────────────────────────────────
  const handleSend = useCallback(
    (text: string, files?: { name: string; path: string }[]) => {
      userScrolledUpRef.current = false;
      send(text, files);
    },
    [send],
  );

  const handleCancel = useCallback(() => {
    cancel();
  }, [cancel]);

  // ── Live Stream Virtual Message ───────────────────────────────────────────
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
    const activeRequestId = routeSessions["l1"]?.requestId;
    const requestOwnedLocally = wsManager.hasChatHandler(activeRequestId);
    return recoverInFlightMessages(
      currentMessages,
      streamChatSegments,
      isL1Session &&
        l1AgentState === "processing" &&
        !!activeRequestId &&
        !requestOwnedLocally,
    );
  }, [
    currentMessages,
    isL1Session,
    l1AgentState,
    routeSessions,
    streamChatSegments,
  ]);

  const pendingConfirm = useMemo(() => {
    const lastMessage = finalMessages[finalMessages.length - 1];
    if (lastMessage && lastMessage.role === "assistant") {
      return lastMessage.segments.find(
        (seg) => seg.type === "tool_confirm" && !seg.resolved
      ) as Extract<ChatSegment, { type: "tool_confirm" }> | undefined;
    }
    return undefined;
  }, [finalMessages]);

  const isHistoryLoading = historyLoading["l1"] ?? false;

  const requestActive = (streaming || delegating) && !isSystemCommandRunning;
  const activeRoute = routeSessions["l1"];
  const routeResolved = !!activeRoute?.taskLevel && !!activeRoute?.modelId;
  const inputModelName = requestActive ? (routeResolved ? activeRoute.modelId : "") : undefined;
  const inputTaskLevel = requestActive ? (routeResolved ? activeRoute.taskLevel : "") : undefined;

  // ── Render ────────────────────────────────────────────────────────────────

  return (
    <div className="flex h-full w-full overflow-hidden bg-background">
      <div className="flex flex-1 flex-col overflow-hidden h-full bg-background relative">
        {/* Header — matches ChatPage header style, respects sidebar collapsed state */}
        <header
          className={cn(
            "flex h-12 shrink-0 items-center border-b border-border/30 bg-card/20 select-none",
            sidebarCollapsed ? "pl-[115px]" : "px-6",
          )}
        >
          <div className="flex flex-1 items-center gap-3 px-6 h-full">
            <h1 className="text-xs font-bold text-foreground font-mono truncate">
              {agentName}
            </h1>
          </div>
        </header>

        {/* Messages — conditional overflow to avoid scrollbar when empty */}
        <div
          ref={scrollRef}
          className={
            finalMessages.length > 0 ? "flex-1 overflow-y-auto" : "flex-1"
          }
        >
          {finalMessages.length === 0 && isHistoryLoading ? (
            <div className="flex h-full flex-col items-center justify-center gap-4 px-6 select-none">
              <Loader2 className="h-7 w-7 animate-spin text-signal/70" />
              <p className="text-xs text-muted-foreground font-mono">
                Loading history...
              </p>
            </div>
          ) : finalMessages.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center gap-4 px-6 select-none">
              <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-primary/10 border border-primary/20">
                <Sparkles className="h-7 w-7 text-primary" />
              </div>
              <h2 className="text-lg font-semibold text-foreground/80">
                {agentName}
              </h2>
              <p className="max-w-xs text-center text-xs text-muted-foreground">
                Send a message to the L1 agent for an instant response.
              </p>
            </div>
          ) : (
            <div className="mx-auto max-w-3xl">
              {isLoadingMore && (
                <div className="flex items-center justify-center py-4">
                  <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                  <span className="text-xs text-muted-foreground font-mono ml-2">
                    Loading more history...
                  </span>
                </div>
              )}
              {finalMessages.map((msg) => (
                <ChatMessageView
                  key={msg.id}
                  message={msg}
                  agentName={agentName}
                  onUserInteraction={handleUserInteraction}
                  modelName={msg.role === 'assistant' ? inputModelName : undefined}
                />
              ))}
            </div>
          )}
          {requestActive && (
            <div className="mx-auto max-w-3xl px-4 w-full">
              <AgentWorkingIndicator
                agentName={agentName}
                modelName={inputModelName}
                taskLevel={inputTaskLevel}
                delegating={delegating}
                compact={false}
              />
            </div>
          )}
          <div ref={bottomRef} className="h-2" />
        </div>

        {pendingConfirm && (
          <StickyToolConfirmPanel pendingConfirm={pendingConfirm} />
        )}

        {/* Input — same ChatInput as ChatPage */}
        <ChatInput
          onSend={handleSend}
          onCancel={handleCancel}
          streaming={streaming}
          delegating={delegating}
          disabled={connectionStatus !== "connected"}
          showL2Selectors={false}
          ctxwinUsed={ctxwinUsed}
          ctxwinLimit={ctxwinLimit}
          processing={streaming || delegating || !!pendingConfirm}
          skillNames={filteredSkillNames}
          activeSessionId="l1"
        />
      </div>
    </div>
  );
}
