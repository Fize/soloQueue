import type { AgentStreamState, ChatMessage, ChatSegment } from "@/types";

export type RequestOwnedChatMessage = ChatMessage & { requestId?: string };

export interface RequestStreamRecovery {
  requestId: string;
  segments: ChatSegment[];
  startedAt?: string;
  terminal?: boolean;
}

/** Return only the stream owned by this exact agent instance and request. */
export function findRequestStream(
  streams: AgentStreamState[],
  agentId: string | null | undefined,
  requestId: string | null | undefined,
): AgentStreamState | undefined {
  if (!agentId || !requestId) return undefined;
  return streams.find(
    (stream) => stream.agent_id === agentId && stream.request_id === requestId,
  );
}

const requestMessagePrefix = "msg-";

function proseText(segments: ChatSegment[]): string {
  return segments
    .filter((segment): segment is Extract<ChatSegment, { type: "content" }> => segment.type === "content")
    .map((segment) => segment.text)
    .join("");
}

function thinkingText(segments: ChatSegment[]): string {
  return segments
    .filter((segment): segment is Extract<ChatSegment, { type: "thinking" }> => segment.type === "thinking")
    .map((segment) => segment.text)
    .join("");
}

export function recoverInFlightMessages(
  currentMessages: ChatMessage[],
  streamSegments: ChatSegment[],
  shouldRecover: boolean,
  requestId?: string,
  startedAt?: string,
  terminal?: boolean,
): ChatMessage[] {
  if (!shouldRecover || !requestId || streamSegments.length === 0) {
    return currentMessages;
  }

  const streamMessageId = `msg-${requestId}`;

  // A history API row has no request ID. For text-only streams, require both
  // the exact persisted text and one bounded, unowned candidate in the
  // request's observed time interval. A later unrelated row must not make a
  // terminal recovery disappear merely because it was written after start.
  const streamHasToolCall = streamSegments.some((segment) => segment.type === "tool_call");
  if (!streamHasToolCall && startedAt) {
    const startedAtMs = Date.parse(startedAt);
    if (!Number.isNaN(startedAtMs)) {
      const streamProse = proseText(streamSegments);
      const streamThinking = thinkingText(streamSegments);
      const streamText = streamProse || streamThinking;
      const streamTextType = streamProse ? "content" : "thinking";
      const nowMs = Date.now();
      const candidates = currentMessages.filter((message) => {
        if (message.role !== "assistant" || message.id.startsWith(requestMessagePrefix)) return false;
        if ((message as RequestOwnedChatMessage).requestId) return false;
        const timestamp = Date.parse(message.timestamp);
        const sameText = streamTextType === "content"
          ? proseText(message.segments) === streamText
          : message.segments.every((segment) => segment.type === "thinking") && thinkingText(message.segments) === streamText;
        return !Number.isNaN(timestamp) && timestamp >= startedAtMs && timestamp <= nowMs &&
          streamText.length > 0 && sameText;
      });
      if (terminal && candidates.length === 1) {
        // History has explicitly taken over this terminal request. Keep the
        // persisted answer instead of replacing it with the synthetic state
        // placeholder used while history was still unavailable.
        return currentMessages;
      }
      if (candidates.length === 1) {
        const candidate = candidates[0];
        const updated = [...currentMessages];
        const index = updated.indexOf(candidate);
        updated[index] = { ...candidate, segments: streamSegments, requestId } as RequestOwnedChatMessage;
        return updated;
      }
    }
  }

  const streamCalls = new Map(streamSegments.flatMap((segment) =>
    segment.type === "tool_call" && segment.callId ? [[segment.callId, segment] as const] : [],
  ));
  const matchingRows = currentMessages.filter((message) => {
    const owner = (message as RequestOwnedChatMessage).requestId;
    return message.role === "assistant" &&
      (!owner || owner === requestId) &&
      !message.id.startsWith(requestMessagePrefix) &&
      message.segments.some((segment) => segment.type === "tool_call" && streamCalls.has(segment.callId));
  });
  const hasVirtualCopy = matchingRows.length > 0 && currentMessages.some((message) => message.id === streamMessageId);
  const callsOnly = streamSegments.every((segment) => segment.type === "tool_call") &&
    matchingRows.some((message) => !(message as RequestOwnedChatMessage).requestId);
  // A shared call does not establish ownership of historical prose. Only
  // replace an unowned row wholesale when its text also exists in the stream.
  const hasUnownedText = matchingRows.some((message) =>
    !(message as RequestOwnedChatMessage).requestId && message.segments.some((segment) =>
      "text" in segment && !streamSegments.some((current) =>
        current.type === segment.type && "text" in current && current.text === segment.text,
      ),
    ),
  );
  const mixedHistory = hasVirtualCopy || callsOnly || hasUnownedText || matchingRows.length > 1 || matchingRows.some((message) =>
    message.segments.some((segment) =>
      segment.type === "tool_call" && !streamCalls.has(segment.callId),
    ),
  );
  if (mixedHistory) {
    // A merged history row or a stream spanning a follow-up does not prove
    // whole-message ownership. Update only the proven calls where they are;
    // recover any remaining output separately without duplicating those cards.
    const reconciledCallIDs = new Set<string>();
    currentMessages = currentMessages.map((message) => {
      if (!matchingRows.includes(message)) return message;
      return {
        ...message,
        segments: message.segments.map((segment) => {
          if (segment.type !== "tool_call") return segment;
          const current = streamCalls.get(segment.callId);
          if (!current) return segment;
          reconciledCallIDs.add(segment.callId);
          return current;
        }),
      };
    });
    streamSegments = streamSegments.filter((segment) =>
      segment.type !== "tool_call" || !reconciledCallIDs.has(segment.callId),
    );
    // A previous renderer may already have appended a virtual copy. Remove
    // only its proven duplicate calls, retaining any other output in place.
    currentMessages = currentMessages.flatMap((message) => {
      if (message.id !== streamMessageId) return [message];
      const segments = message.segments.filter((segment) =>
        segment.type !== "tool_call" || !reconciledCallIDs.has(segment.callId),
      );
      return segments.length ? [{ ...message, segments }] : [];
    });
    if (streamSegments.length === 0) return currentMessages;
  }

  // A request-owned virtual message is updated in place on every runtime
  // snapshot. This keeps recovery idempotent across reconnects and renders.
  const existingIndex = currentMessages.findIndex((message) =>
    message.id === streamMessageId ||
    (!mixedHistory && (message as RequestOwnedChatMessage).requestId === requestId),
  );
  if (existingIndex >= 0) {
    const updated = [...currentMessages];
    updated[existingIndex] = {
      ...updated[existingIndex],
      role: "assistant",
      segments: streamSegments,
      requestId,
    } as RequestOwnedChatMessage;
    return updated;
  }

  // History IDs differ from runtime IDs, and a follow-up user message can
  // separate an active delegation from the tail. Exact tool call identities
  // let us recover that reply in place without attaching it to the follow-up.
  // Require all calls in the row to belong to this stream: history can merge
  // adjacent assistant iterations, including unrelated concurrent requests.
  const streamCallIDs = new Set(streamSegments.flatMap((segment) =>
    segment.type === "tool_call" && segment.callId ? [segment.callId] : [],
  ));
  const ownerIndex = currentMessages.findIndex((message) => {
    if (message.role !== "assistant") return false;
    const owner = (message as RequestOwnedChatMessage).requestId;
    if (owner && owner !== requestId) return false;
    if (message.id.startsWith(requestMessagePrefix)) return false;
    const calls = message.segments.filter((segment) => segment.type === "tool_call");
    return calls.length > 0 && calls.every((call) =>
      !!call.callId && streamCallIDs.has(call.callId),
    );
  });
  if (ownerIndex >= 0) {
    const updated = [...currentMessages];
    updated[ownerIndex] = {
      ...updated[ownerIndex],
      segments: streamSegments,
      requestId,
    } as RequestOwnedChatMessage;
    return updated;
  }

  // A trailing history row has no request ownership merely because it is
  // last. Channel streams can arrive before history refresh (or finish out of
  // order), so preserve it unless the exact request/call checks above matched.
  return [
    ...currentMessages,
    {
      id: streamMessageId,
      role: "assistant",
      segments: streamSegments,
      timestamp: new Date().toISOString(),
      requestId,
    } as RequestOwnedChatMessage,
  ];
}

/**
 * Hydrate every request-owned runtime stream that is no longer handled by the
 * current renderer. A session can have several active L1 requests, so callers
 * must recover by request identity instead of choosing one route/session ID.
 */
export function recoverInFlightMessageStreams(
  currentMessages: ChatMessage[],
  streams: RequestStreamRecovery[],
  shouldRecover: (requestId: string) => boolean,
): ChatMessage[] {
  return streams.reduce((messages, stream) => {
    if (!stream.requestId || !shouldRecover(stream.requestId)) {
      return messages;
    }
    return recoverInFlightMessages(messages, stream.segments, true, stream.requestId, stream.startedAt, stream.terminal);
  }, currentMessages);
}
