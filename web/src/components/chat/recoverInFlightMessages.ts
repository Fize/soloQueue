import type { ChatMessage, ChatSegment } from "@/types";

export function recoverInFlightMessages(
  currentMessages: ChatMessage[],
  streamSegments: ChatSegment[],
  shouldRecover: boolean,
): ChatMessage[] {
  if (!shouldRecover || streamSegments.length === 0) {
    return currentMessages;
  }

  // Timeline hydration may already contain a partial assistant snapshot for
  // this turn. Replace only the trailing assistant run with the authoritative
  // runtime stream, preserving all earlier user/assistant history.
  let base = currentMessages;
  while (base.length > 0 && base[base.length - 1].role === "assistant") {
    base = base.slice(0, -1);
  }

  return [
    ...base,
    {
      id: "msg-virtual-stream",
      role: "assistant",
      segments: streamSegments,
      timestamp: new Date().toISOString(),
    },
  ];
}
