import { describe, expect, it } from "vitest";
import type { ChatMessage, ChatSegment } from "@/types";
import { recoverInFlightMessages } from "./recoverInFlightMessages";

describe("recoverInFlightMessages", () => {
  const history: ChatMessage[] = [
    {
      id: "old-assistant",
      role: "assistant",
      timestamp: "",
      segments: [{ type: "content", text: "older answer" }],
    },
    {
      id: "current-user",
      role: "user",
      timestamp: "",
      segments: [{ type: "content", text: "current prompt" }],
    },
    {
      id: "partial-assistant",
      role: "assistant",
      timestamp: "",
      segments: [{ type: "thinking", text: "stale partial" }],
    },
  ];
  const runtimeStream: ChatSegment[] = [
    { type: "thinking", text: "recovered reasoning" },
    { type: "content", text: "recovered answer" },
  ];

  it("replaces the trailing partial assistant after a renderer reload", () => {
    const messages = recoverInFlightMessages(history, runtimeStream, true);

    expect(messages.map((message) => message.id)).toEqual([
      "old-assistant",
      "current-user",
      "msg-virtual-stream",
    ]);
    expect(messages.at(-1)?.segments).toEqual(runtimeStream);
  });

  it("leaves handler-owned live messages unchanged", () => {
    expect(recoverInFlightMessages(history, runtimeStream, false)).toBe(history);
  });
});
