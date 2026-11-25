import { useEffect, useRef, useCallback, useMemo, useState, useLayoutEffect } from "react";
import { ChatMessageView } from "@/components/ChatMessage";
import { CompactionStatusRows } from "@/components/chat/CompactionStatusRows";
import { ChatInput } from "@/components/ChatInput";
import { AgentWorkingIndicator } from "@/components/chat/AgentWorkingIndicator";
import { Sparkles, Loader2 } from "lucide-react";
import { useChatStore } from "@/stores/chatStore";
import { useChatStream } from "@/hooks/useChatStream";
import { useTranscript } from "@/hooks/useTranscript";
import { compactionEventRequests } from "@/lib/transcript/messages";
import { useShallow } from "zustand/react/shallow";
import { useAgentStore } from "@/stores/agentStore";
import { runtimeSessionId, useRuntimeStore } from "@/stores/runtimeStore";
import { cn } from "@/lib/utils";
import { getSkills } from "@/lib/api";
import type { SkillInfo } from "@/types";
import { useStickToBottom } from "@/hooks/useStickToBottom";
import { useTranslation } from "@/lib/i18n";

export function AssistantPage() {
  const { t } = useTranslation();
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const compactions = useChatStore((s) => s.compactions);
  const streamingSessions = useChatStore(useShallow((s) => s.streamingSessions));
  const systemCommandSessions = useChatStore(useShallow((s) => s.systemCommandSessions));
  const delegatingSessions = useChatStore(useShallow((s) => s.delegatingSessions));
  const routeSessions = useChatStore(useShallow((s) => s.routeSessions));
  const activeRequests = useChatStore(useShallow((s) => s.activeRequests));
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const transcript = useTranscript("l1");

  const delegating = activeSessionId ? (delegatingSessions[activeSessionId] ?? false) : false;
  const isSystemCommandRunning = activeSessionId
    ? (systemCommandSessions[activeSessionId] ?? false)
    : false;

  const { send, cancel } = useChatStream();
  const scrollRef = useRef<HTMLDivElement>(null);
  const { contentRef, followOutput, resetFollow, detachFollow, showReturnToLatest, jumpToLatest } = useStickToBottom({ scrollRef });
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

  const l1Agent = useMemo(() => {
    return agentsData?.agents.find((a) => a.id === "l1-agent") || null;
  }, [agentsData]);
  const l1AgentInstanceId = l1Agent?.instance_id || null;

  const filteredSkillNames = useMemo(() => {
    return skills.map((s) => s.name);
  }, [skills]);

  const refreshSkills = useCallback(() => {
    getSkills()
      .then((resp) => setSkills(resp.skills || []))
      .catch(() => {});
  }, []);

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

  // ── Load more history when scrolling up ───────────────────────────────────
  const hasMore = transcript.hasMore;
  const isLoadingMore = transcript.loadingOlder;
  const loadOlder = transcript.loadOlder;

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
        loadOlder().then(() => {
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
  }, [hasMore, isLoadingMore, loadOlder]);


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

  const finalMessages = transcript.messages;

  useLayoutEffect(() => {
    if (finalMessages.length === 0) return;
    followOutput();
  }, [finalMessages, followOutput]);

  const isHistoryLoading = transcript.status === "loading";

  const requestActive = (streaming || delegating || runtimeProcessing || runtimeDelegating) && !isSystemCommandRunning;
  const activeRoute = routeSessions["l1"];
  const inputProviderName = requestActive ? (effectiveRequestRuntime?.provider_id || activeRoute?.providerId || "") : undefined;
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
                <CompactionStatusRows
                  rows={compactions[activeSessionId || 'l1'] || []}
                  committedRequestIds={compactionEventRequests(finalMessages)}
                />
              </div>
              {requestActive && (
                <div className="mx-auto max-w-3xl px-4 w-full">
                  <AgentWorkingIndicator
                    agentName={agentName}
                    providerName={inputProviderName}
                    modelName={inputModelName}
                    taskLevel={inputTaskLevel}
                    delegating={delegating || runtimeDelegating}
                    hideRouteInfo
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
                  providerName={inputProviderName}
                  modelName={inputModelName}
                  taskLevel={inputTaskLevel}
                  delegating={delegating || runtimeDelegating}
                  compact={false}
                />
              </div>
              <div className="h-2" />
            </div>
          )}
          {showReturnToLatest && <button type="button" onClick={jumpToLatest} className="sticky bottom-3 z-10 mx-auto block rounded-full border border-border bg-card px-4 py-2 text-xs font-medium text-foreground shadow-md hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring">{t("chat.returnToLatest")}</button>}
        </div>


        {/* Input — same ChatInput as ChatPage */}
        <div className="chat-composer" data-chat-composer>
          <div className="mx-auto w-full max-w-3xl px-4">
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
              modelName={inputModelName}
              taskLevel={inputTaskLevel}
              skillNames={filteredSkillNames}
              onRefreshSkills={refreshSkills}
              activeSessionId="l1"
            />
          </div>
        </div>
      </div>
    </div>
  );
}
