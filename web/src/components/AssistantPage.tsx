import { useWorkedStateKeys } from "@/hooks/useWorkedStateKeys";
import { useEffect, useRef, useCallback, useMemo, useState, useLayoutEffect } from "react";
import { ChatMessageView } from "@/components/ChatMessage";
import { ChatInput } from "@/components/ChatInput";
import { AgentWorkingIndicator } from "@/components/chat/AgentWorkingIndicator";
import { Sparkles, Loader2 } from "lucide-react";
import { useChatStore } from "@/stores/chatStore";
import { useChatStream } from "@/hooks/useChatStream";
import { useAgentStream } from "@/hooks/useAgentStream";
import { useAgentStore } from "@/stores/agentStore";
import { runtimeSessionId, useRuntimeStore } from "@/stores/runtimeStore";
import { cn } from "@/lib/utils";
import { getSkills } from "@/lib/api";
import { wsManager } from "@/lib/websocket";
import type { SkillInfo, ChatSegment } from "@/types";
import {
  findRequestStream,
  recoverInFlightMessageStreams,
} from "@/components/chat/recoverInFlightMessages";
import type { RequestStreamRecovery } from "@/components/chat/recoverInFlightMessages";
import { useStickToBottom } from "@/hooks/useStickToBottom";
import { useTranslation } from "@/lib/i18n";

export function AssistantPage() {
  const { t } = useTranslation();
  const {
    activeSessionId,
    messages,
    streamingSessions,
    systemCommandSessions,
    delegatingSessions,
    routeSessions,
    activeRequests,
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
  const { contentRef, followOutput, resetFollow, detachFollow } = useStickToBottom({ scrollRef });
  const loadingMoreRef = useRef(false);

  const handleUserInteraction = useCallback(() => {
    detachFollow();
  }, [detachFollow]);

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

  const agentName = t('sidebar.assistant');

  const isL1Session = activeSessionId === "l1";
  const l1Agent = useMemo(() => {
    return agentsData?.agents.find((a) => a.id === "l1-agent") || null;
  }, [agentsData]);
  const l1AgentState = l1Agent?.state;
  const l1AgentInstanceId = l1Agent?.instance_id || null;
  const liveRequestId = routeSessions["l1"]?.requestId;
  const stream = useAgentStream(l1AgentInstanceId, liveRequestId);

  // L1 may execute several requests at once. The selected route is only
  // suitable for input metadata; recovery must use every active request from
  // runtime state and preserve each request's own virtual assistant message.
  const recoveryRequestIDs = useMemo(() => {
    const ids = new Set<string>();

    for (const request of Object.values(activeRequests ?? {})) {
      if (request.sessionId === "l1") ids.add(request.requestId);
    }

    for (const [key, runtime] of Object.entries(runtimeStatus?.sessions ?? {})) {
      const runtimeSessionID = runtimeSessionId(key, runtime);
      if (
        runtimeSessionID === "l1" &&
        runtime.request_id &&
        ((runtime.state !== "idle" && runtime.state !== "error") || runtime.terminal_code)
      ) {
        ids.add(runtime.request_id);
      }
    }

    if (liveRequestId && (l1AgentState === "processing" || streaming || delegating)) {
      ids.add(liveRequestId);
    }

    for (const streamState of Object.values(runtimeStatus?.agent_streams ?? {})) {
      if (
        (streamState.processing || !!streamState.error) &&
        streamState.request_id &&
        !!l1AgentInstanceId &&
        streamState.agent_id === l1AgentInstanceId
      ) {
        ids.add(streamState.request_id);
      }
    }

    return [...ids];
  }, [
    activeRequests,
    delegating,
    l1AgentInstanceId,
    l1AgentState,
    liveRequestId,
    runtimeStatus,
    streaming,
  ]);

  const filteredSkillNames = useMemo(() => {
    return skills.map((s) => s.name);
  }, [skills]);

  // Context window tokens
  const selectedRuntimeRequestId = routeSessions["l1"]?.requestId || Object.values(activeRequests ?? {}).find((request) => request.sessionId === "l1")?.requestId;
  const runtimeRequests = Object.entries(runtimeStatus?.sessions ?? {})
    .filter(([key, runtime]) => runtimeSessionId(key, runtime) === "l1")
    .map(([, runtime]) => runtime);
  const requestRuntime = selectedRuntimeRequestId
    ? runtimeRequests.find((runtime) => runtime.request_id === selectedRuntimeRequestId)
    : undefined;
  const effectiveRequestRuntime = requestRuntime || runtimeRequests.find((runtime) => runtime.state !== "idle" && runtime.state !== "error") || runtimeRequests[0];
  const runtimeProcessing = runtimeRequests.some((runtime) => runtime.state !== "idle" && runtime.state !== "error");
  const runtimeDelegating = runtimeRequests.some((runtime) => runtime.delegating);
  const l1Runtime = effectiveRequestRuntime || runtimeStatus?.sessions?.l1;
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

  const hydratedTerminalRequests = useRef(new Set<string>());
  useEffect(() => {
    if (!isL1Session) return;
    for (const [key, runtime] of Object.entries(runtimeStatus?.sessions ?? {})) {
      if (runtimeSessionId(key, runtime) !== "l1" || !runtime.request_id || !runtime.terminal_code) continue;
      const terminalKey = `l1:${runtime.request_id}:${runtime.terminal_code}`;
      if (hydratedTerminalRequests.current.has(terminalKey)) continue;
      hydratedTerminalRequests.current.add(terminalKey);
      loadHistory("l1");
    }
  }, [isL1Session, loadHistory, runtimeStatus]);

  // ── Load more history when scrolling up ───────────────────────────────────
  const hasMore = historyHasMore["l1"] ?? false;
  const isLoadingMore = historyLoading["l1"] ?? false;

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const handleScroll = () => {
      const { scrollTop, scrollHeight } = el;

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

  const currentMessages = useMemo(() => messages["l1"] ?? [], [messages]);

  // ── Send & Cancel ─────────────────────────────────────────────────────────
  const handleSend = useCallback(
    (text: string, files?: { name: string; path: string }[]) => {
      resetFollow();
      send(text, files);
    },
    [resetFollow, send],
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
          agentInstanceId: seg.agent_instance_id || undefined,
          result: seg.result || undefined,
          error: seg.error || undefined,
          durationMs: seg.duration_ms || undefined,
          done: seg.done,
        };
      }
      return seg;
    });
  }, [stream]);

  const recoveryStreams = useMemo<RequestStreamRecovery[]>(() => {
    const runtimeStreams = Object.values(runtimeStatus?.agent_streams ?? {});
    return recoveryRequestIDs.flatMap((requestId) => {
      const runtimeRequest = Object.entries(runtimeStatus?.sessions ?? {})
        .find(([key, runtime]) => runtime.request_id === requestId && runtimeSessionId(key, runtime) === "l1")?.[1];
      const runtimeStream = findRequestStream(runtimeStreams, l1AgentInstanceId, requestId);
      if (runtimeStream?.segments.length) {
        return [{
          requestId,
          startedAt: runtimeRequest?.started_at,
          segments: runtimeStream.segments.map((seg): ChatSegment => {
            if (seg.type === "tool_call") {
              return {
                type: "tool_call" as const,
                callId: seg.call_id,
                name: seg.name,
                args: seg.args,
                agentInstanceId: seg.agent_instance_id || undefined,
                result: seg.result || undefined,
                error: seg.error || undefined,
                durationMs: seg.duration_ms || undefined,
                done: seg.done,
              };
            }
            return seg;
          }),
        }];
      }
      if (runtimeStream?.error || runtimeRequest?.terminal_code) {
        const message = runtimeStream?.error || runtimeRequest?.error ||
          (runtimeRequest?.terminal_code === "completed" ? "Response completed; loading history…" : `Request ${runtimeRequest?.terminal_code || "ended"}.`);
        const terminalSegment = runtimeStream?.error || runtimeRequest?.error
          ? { type: "error" as const, text: message }
          : { type: "content" as const, text: message };
        return [{
          requestId,
          startedAt: runtimeRequest?.started_at,
          terminal: true,
          segments: [terminalSegment],
        }];
      }
      if (requestId === liveRequestId && stream?.segments?.length) {
        return [{ requestId, segments: streamChatSegments, startedAt: runtimeRequest?.started_at }];
      }
      return [];
    });
  }, [
    l1AgentInstanceId,
    liveRequestId,
    recoveryRequestIDs,
    runtimeStatus,
    stream,
    streamChatSegments,
  ]);

  const recoveredMessages = useMemo(() => {
    return recoverInFlightMessageStreams(
      currentMessages,
      recoveryStreams,
      (requestId) => !wsManager.hasChatHandler(requestId),
    );
  }, [
    currentMessages,
    recoveryStreams,
  ]);

  const finalMessages = useWorkedStateKeys("l1", recoveredMessages);

  useLayoutEffect(() => {
    if (finalMessages.length === 0) return;
    followOutput();
  }, [finalMessages, followOutput]);

  const isHistoryLoading = historyLoading["l1"] ?? false;

  const requestActive = (streaming || delegating || runtimeProcessing || runtimeDelegating) && !isSystemCommandRunning;
  const activeRoute = routeSessions["l1"];
  const inputModelName = requestActive ? (effectiveRequestRuntime?.model_id || activeRoute?.modelId || "") : undefined;
  const inputTaskLevel = requestActive ? (effectiveRequestRuntime?.task_type || activeRoute?.taskLevel || "") : undefined;

  // ── Render ────────────────────────────────────────────────────────────────

  return (
    <div className="flex h-full w-full overflow-hidden bg-background">
      <div className="chat-layout chat-layout-with-header flex-1 h-full bg-background relative">
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
          data-chat-viewport
          className="chat-output pb-4"
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
                {t('chat.assistantDesc')}
              </p>
            </div>
          ) : (
            <div ref={contentRef}>
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
                    sessionId="l1"
                    sessionAgentId={l1AgentInstanceId}
                  />
                ))}
              </div>
              {requestActive && (
                <div className="mx-auto max-w-3xl px-4 w-full">
                  <AgentWorkingIndicator
                    agentName={agentName}
                    modelName={inputModelName}
                    taskLevel={inputTaskLevel}
                    delegating={delegating || runtimeDelegating}
                    compact={false}
                  />
                </div>
              )}
              <div className="h-2" />
            </div>
          )}
          {finalMessages.length === 0 && requestActive && (
            <div ref={contentRef}>
              <div className="mx-auto max-w-3xl px-4 w-full">
                <AgentWorkingIndicator
                  agentName={agentName}
                  modelName={inputModelName}
                  taskLevel={inputTaskLevel}
                  delegating={delegating || runtimeDelegating}
                  compact={false}
                />
              </div>
              <div className="h-2" />
            </div>
          )}
        </div>


        {/* Input — same ChatInput as ChatPage */}
        <div className="chat-composer" data-chat-composer>
          <ChatInput
            onSend={handleSend}
            onCancel={handleCancel}
            streaming={streaming}
            delegating={delegating}
            disabled={connectionStatus !== "connected"}
            showL2Selectors={false}
            ctxwinUsed={ctxwinUsed}
            ctxwinLimit={ctxwinLimit}
            processing={requestActive}
            skillNames={filteredSkillNames}
            activeSessionId="l1"
          />
        </div>
      </div>
    </div>
  );
}
