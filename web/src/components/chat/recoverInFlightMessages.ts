import type { ChatMessage, ChatSegment } from "@/types";

export function recoverInFlightMessages(
  currentMessages: ChatMessage[],
  streamSegments: ChatSegment[],
  shouldRecover: boolean,
  requestId?: string,
): ChatMessage[] {
  if (!shouldRecover || streamSegments.length === 0) {
    return currentMessages;
  }

  const streamMessageId = requestId ? `msg-${requestId}` : "msg-virtual-stream";

  // A request-owned virtual message is updated in place on every runtime
  // snapshot. This keeps recovery idempotent across reconnects and renders.
  const existingIndex = currentMessages.findIndex((message) => message.id === streamMessageId);
  if (existingIndex >= 0) {
    const updated = [...currentMessages];
    updated[existingIndex] = {
      ...updated[existingIndex],
      role: "assistant",
      segments: streamSegments,
    };
    return updated;
  }

  // Timeline hydration may already contain a partial assistant snapshot for
  // this request. Replace only the trailing assistant run with the
  // authoritative runtime stream, preserving all earlier user/assistant
  // history. The caller must provide a request identity before using this
  // fallback; an unscoped global stream is not safe to attach to the latest
  // user message when L1 has concurrent requests.
  let base = currentMessages;
  while (base.length > 0 && base[base.length - 1].role === "assistant") {
    base = base.slice(0, -1);
  }

  return [
    ...base,
    {
      id: streamMessageId,
      role: "assistant",
      segments: streamSegments,
      timestamp: new Date().toISOString(),
    },
  ];
}
