import { describe, expect, it } from "vitest";
import type { ChatMessage, ChatSegment } from "@/types";
import {
  findRequestStream,
  recoverInFlightMessageStreams,
  recoverInFlightMessages,
} from "./recoverInFlightMessages";

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

  it("does not recover an unscoped stream after a renderer reload", () => {
    const messages = recoverInFlightMessages(history, runtimeStream, true);

    expect(messages).toBe(history);
  });

  it("leaves handler-owned live messages unchanged", () => {
    expect(recoverInFlightMessages(history, runtimeStream, false)).toBe(history);
  });

  it("keeps request-scoped recovery idempotent", () => {
    const first = recoverInFlightMessages(history, runtimeStream, true, "req-new");
    const second = recoverInFlightMessages(first, [
      { type: "content", text: "updated answer" },
    ], true, "req-new");

    expect(second.map((message) => message.id)).toEqual([
      "old-assistant",
      "current-user",
      "partial-assistant",
      "msg-req-new",
    ]);
    expect(second.at(-1)?.segments).toEqual([
      { type: "content", text: "updated answer" },
    ]);
  });

  it("never replaces unowned trailing history merely because a new request is active", () => {
    const completed: ChatMessage = { id: "hist-completed", role: "assistant", segments: [{ type: "content", text: "completed reply" }] };
    const result = recoverInFlightMessages([completed], runtimeStream, true, "new-channel-request");
    expect(result[0]).toBe(completed);
    expect(result).toHaveLength(2);
    expect(recoverInFlightMessages(result, runtimeStream, true, "new-channel-request")).toEqual(result);
  });

  it("preserves another concurrent request's trailing assistant message", () => {
    const concurrentHistory: ChatMessage[] = [
      ...history,
      {
        id: "msg-req-other",
        role: "assistant",
        timestamp: "",
        segments: [{ type: "content", text: "other request answer" }],
      },
    ];

    const messages = recoverInFlightMessages(
      concurrentHistory,
      runtimeStream,
      true,
      "req-new",
    );

    expect(messages.map((message) => message.id)).toEqual([
      "old-assistant",
      "current-user",
      "partial-assistant",
      "msg-req-other",
      "msg-req-new",
    ]);
  });

  it("recovers every active request stream independently", () => {
    const messages = recoverInFlightMessageStreams(
      history,
      [
        {
          requestId: "req-a",
          segments: [{ type: "content", text: "request A answer" }],
        },
        {
          requestId: "req-b",
          segments: [{ type: "content", text: "request B answer" }],
        },
      ],
      () => true,
    );

    expect(messages.map((message) => message.id)).toEqual([
      "old-assistant",
      "current-user",
      "partial-assistant",
      "msg-req-a",
      "msg-req-b",
    ]);
    expect(messages.at(-2)?.segments).toEqual([
      { type: "content", text: "request A answer" },
    ]);
    expect(messages.at(-1)?.segments).toEqual([
      { type: "content", text: "request B answer" },
    ]);
    expect((messages.at(-1) as ChatMessage & { requestId?: string }).requestId).toBe("req-b");
  });

  it("keeps a terminal request visible when runtime has no segments", () => {
    const first = recoverInFlightMessageStreams([], [{ requestId: "req-error", terminal: true, segments: [{ type: "error", text: "cancelled" }] }], () => true);
    expect(first).toHaveLength(1);
    expect(first[0]).toMatchObject({ id: "msg-req-error", requestId: "req-error" });
    expect(recoverInFlightMessageStreams(first, [{ requestId: "req-error", terminal: true, segments: [{ type: "error", text: "cancelled" }] }], () => true)).toEqual(first);

    const history: ChatMessage = { id: "hist-1", role: "assistant", timestamp: "2026-09-18T10:00:02.000Z", segments: [{ type: "content", text: "persisted answer" }] };
    const terminal = [{ requestId: "req-done", terminal: true, startedAt: "2026-09-18T10:00:00.000Z", segments: [{ type: "content" as const, text: "Response completed; loading history…" }] }];
    const recovered = recoverInFlightMessageStreams([history], terminal, () => true);
    expect(recovered.map((message) => message.id)).toEqual(["hist-1", "msg-req-done"]);

    const exact = { ...history, segments: [{ type: "content" as const, text: "Response completed; loading history…" }] };
    expect(recoverInFlightMessageStreams([exact], terminal, () => true)).toEqual([exact]);
  });

  it("matches one prose history row by request start time but keeps ambiguous same text independent", () => {
    const historyRow: ChatMessage = { id: "hist-1", role: "assistant", timestamp: "2026-09-18T10:00:01.000Z", segments: [{ type: "content", text: "same answer" }] };
    const stream: ChatSegment[] = [{ type: "content", text: "same answer" }];
    const matched = recoverInFlightMessages([historyRow], stream, true, "req-a", "2026-09-18T10:00:00.000Z");
    expect(matched).toHaveLength(1);
    expect((matched[0] as ChatMessage & { requestId?: string }).requestId).toBe("req-a");

    const second: ChatMessage = { ...historyRow, id: "hist-2", timestamp: "2026-09-18T10:00:02.000Z" };
    const independent = recoverInFlightMessages([historyRow, second], stream, true, "req-b", "2026-09-18T10:00:00.000Z");
    expect(independent.map((message) => message.id)).toEqual(["hist-1", "hist-2", "msg-req-b"]);
  });

  it("matches one pure-thinking history row by request start time without duplicating it", () => {
    const historyRow: ChatMessage = {
      id: "hist-thinking",
      role: "assistant",
      timestamp: "2026-09-18T10:00:01.000Z",
      segments: [{ type: "thinking", text: "same reasoning" }],
    };
    const stream: ChatSegment[] = [{ type: "thinking", text: "same reasoning" }];
    const matched = recoverInFlightMessages(
      [historyRow], stream, true, "req-thinking", "2026-09-18T10:00:00.000Z",
    );
    expect(matched).toHaveLength(1);
    expect((matched[0] as ChatMessage & { requestId?: string }).requestId).toBe("req-thinking");
  });

  const delegation = (callId: string): ChatSegment => ({
    type: "tool_call",
    callId,
    name: "delegate",
    args: '{"agent":"Andrej Karpathy","task":"same task"}',
    done: false,
  });

  it("recovers a delegation in its original reply before a later user message", () => {
    const original: ChatMessage = {
      id: "hist-1",
      role: "assistant",
      timestamp: "2026-09-18T11:01:00+08:00",
      segments: [delegation("call-a")],
    };
    const followup: ChatMessage = {
      id: "hist-2",
      role: "user",
      timestamp: "2026-09-18T11:03:00+08:00",
      segments: [{ type: "content", text: "follow up" }],
    };
    const segments: ChatSegment[] = [delegation("call-a"), { type: "content", text: "progress" }];
    const recovered = recoverInFlightMessages([original, followup], segments, true, "req-a");

    expect(recovered).toHaveLength(2);
    expect(recovered[0]).toMatchObject({ id: original.id, timestamp: original.timestamp, requestId: "req-a", segments });
    expect(recovered[1]).toBe(followup);
    const nextSegments: ChatSegment[] = [{ ...delegation("call-a"), done: true } as ChatSegment];
    const next = recoverInFlightMessages(recovered, nextSegments, true, "req-a");
    expect(next).toHaveLength(2);
    expect(next[0].segments).toEqual(nextSegments);
    expect(next[1]).toBe(followup);
  });

  it("preserves an independent historical delegation with identical task text", () => {
    const original: ChatMessage = {
      id: "hist-1", role: "assistant", timestamp: "", segments: [delegation("call-a")],
    };
    const recovered = recoverInFlightMessages([original], [delegation("call-b")], true, "req-b");
    expect(recovered).toHaveLength(2);
    expect(recovered[0]).toBe(original);
    expect(recovered[1].segments).toEqual([delegation("call-b")]);
  });

  it("does not claim a message explicitly owned by another request", () => {
    const original = {
      id: "hist-1", role: "assistant" as const, timestamp: "", requestId: "req-other", segments: [delegation("call-a")],
    };
    const recovered = recoverInFlightMessages([original], [delegation("call-a")], true, "req-a");
    expect(recovered).toHaveLength(2);
    expect(recovered[0]).toBe(original);
  });

  it("does not infer ownership from empty call IDs", () => {
    const original: ChatMessage = {
      id: "hist-1", role: "assistant", timestamp: "", segments: [delegation("")],
    };
    const recovered = recoverInFlightMessages([original], [delegation("")], true, "req-a");
    expect(recovered).toHaveLength(2);
    expect(recovered[0]).toBe(original);
  });

  it("updates shared calls in a mixed history row without removing unrelated segments", () => {
    const unrelated = delegation("call-other");
    const note: ChatSegment = { type: "content", text: "retained history" };
    const original: ChatMessage = {
      id: "hist-1", role: "assistant", timestamp: "", segments: [note, delegation("call-a"), unrelated],
    };
    const completed = { ...delegation("call-a"), done: true } as ChatSegment;
    const recovered = recoverInFlightMessages([original], [completed], true, "req-a");
    expect(recovered).toHaveLength(1);
    expect(recovered[0].segments).toEqual([note, completed, unrelated]);
    expect(recoverInFlightMessages(recovered, [completed], true, "req-a")).toEqual(recovered);
  });

  it("keeps matching calls in their respective history turns", () => {
    const original: ChatMessage = {
      id: "hist-1", role: "assistant", timestamp: "", segments: [delegation("call-a")],
    };
    const followup: ChatMessage = { id: "hist-2", role: "user", timestamp: "", segments: [] };
    const continuation: ChatMessage = {
      id: "hist-3", role: "assistant", timestamp: "", segments: [delegation("call-b")],
    };
    const recovered = recoverInFlightMessages([original, followup, continuation], [delegation("call-a"), delegation("call-b")], true, "req-a");
    expect(recovered).toEqual([original, followup, continuation]);
  });

  it("keeps new output from a mixed row idempotent as runtime snapshots advance", () => {
    const original: ChatMessage = {
      id: "hist-1", role: "assistant", timestamp: "", segments: [delegation("call-a"), delegation("call-other")],
    };
    const segments: ChatSegment[] = [delegation("call-a"), { type: "content", text: "progress" }, delegation("call-new")];
    const first = recoverInFlightMessages([original], segments, true, "req-a");
    const second = recoverInFlightMessages(first, segments, true, "req-a");
    expect(second).toEqual(first);
    expect(second).toHaveLength(2);
    expect(second.flatMap((message) => message.segments).filter((segment) => segment.type === "tool_call").map((segment) => segment.callId)).toEqual(["call-a", "call-other", "call-new"]);
  });

  it.each([false, true])("repairs an existing virtual duplicate while preserving unrelated text (with text: %s)", (withText) => {
    const original: ChatMessage = {
      id: "hist-1", role: "assistant", timestamp: "11:01", segments: [{ type: "content", text: "original" }, delegation("call-a")],
    };
    const followup: ChatMessage = { id: "hist-2", role: "user", timestamp: "11:03", segments: [{ type: "content", text: "followup" }] };
    const duplicate: ChatMessage = {
      id: "msg-req-a", role: "assistant", timestamp: "11:03", segments: [delegation("call-a"), ...(withText ? [{ type: "content" as const, text: "keep this" }] : [])],
    };
    const completed = { ...delegation("call-a"), done: true } as ChatSegment;
    const recovered = recoverInFlightMessages([original, followup, duplicate], [completed], true, "req-a");
    expect(recovered).toHaveLength(withText ? 3 : 2);
    expect(recovered[0].segments).toEqual([{ type: "content", text: "original" }, completed]);
    expect(recovered[1]).toBe(followup);
    if (withText) expect(recovered[2].segments).toEqual([{ type: "content", text: "keep this" }]);
    expect(recovered.flatMap((message) => message.segments).filter((segment) => segment.type === "tool_call" && segment.callId === "call-a")).toHaveLength(1);
    expect(recoverInFlightMessages(recovered, [completed], true, "req-a")).toEqual(recovered);
  });

  it("preserves unrelated historical content when a stream contains reasoning and a shared call", () => {
    const original: ChatMessage = {
      id: "hist-1", role: "assistant", timestamp: "11:01", segments: [{ type: "content", text: "earlier unrelated text" }, delegation("call-a")],
    };
    const completed = { ...delegation("call-a"), done: true } as ChatSegment;
    const runtime: ChatSegment[] = [{ type: "thinking", text: "current" }, completed];
    const recovered = recoverInFlightMessages([original], runtime, true, "req-a");
    expect(recovered[0].segments).toEqual([{ type: "content", text: "earlier unrelated text" }, completed]);
    const allSegments = recovered.flatMap((message) => message.segments);
    expect(allSegments.filter((segment) => segment.type === "tool_call" && segment.callId === "call-a")).toHaveLength(1);
    expect(allSegments).toContainEqual({ type: "thinking", text: "current" });
    expect(recoverInFlightMessages(recovered, runtime, true, "req-a")).toEqual(recovered);
  });

  it("preserves a follow-up answer when mixed history leaves new stream prose", () => {
    const original: ChatMessage = {
      id: "hist-original", role: "assistant", segments: [{ type: "content", text: "old prose" }, delegation("call-a")],
    };
    const followup: ChatMessage = { id: "hist-followup", role: "user", segments: [{ type: "content", text: "follow up" }] };
    const answer: ChatMessage = { id: "hist-answer", role: "assistant", segments: [{ type: "content", text: "independent answer" }] };
    const completed = { ...delegation("call-a"), done: true } as ChatSegment;
    const stream: ChatSegment[] = [completed, { type: "content", text: "new progress" }];
    const recovered = recoverInFlightMessages([original, followup, answer], stream, true, "req-a");
    expect(recovered.map((message) => message.id)).toEqual([original.id, followup.id, answer.id, "msg-req-a"]);
    expect(recovered[0].segments).toEqual([{ type: "content", text: "old prose" }, completed]);
    expect(recovered[2]).toBe(answer);
    expect(recovered[3].segments).toEqual([{ type: "content", text: "new progress" }]);
    expect(recoverInFlightMessages(recovered, stream, true, "req-a")).toEqual(recovered);
  });

  it("does not select a child stream when the parent agent identity is missing", () => {
    const streams = [
      {
        agent_id: "parent-instance",
        request_id: "req-live",
        processing: true,
        segments: [],
        iteration: 1,
      },
      {
        agent_id: "child-instance",
        request_id: "req-live",
        processing: true,
        segments: [],
        iteration: 1,
      },
    ];

    expect(findRequestStream(streams, undefined, "req-live")).toBeUndefined();
    expect(findRequestStream(streams, "parent-instance", "req-live")?.agent_id).toBe(
      "parent-instance",
    );
  });
});
