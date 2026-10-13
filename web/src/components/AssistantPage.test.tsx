import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AssistantPage } from "./AssistantPage";

const mocks = vi.hoisted(() => ({
  loadHistory: vi.fn(),
  loadMoreHistory: vi.fn().mockResolvedValue(undefined),
  setActiveSession: vi.fn(),
  chatInputProps: {} as { modelName?: string; taskLevel?: string },
  agentWorkingProps: {} as { modelName?: string; taskLevel?: string },
}));

const chatStoreState = {
  activeSessionId: "l1",
  messages: { l1: [] },
  streamingSessions: {},
  systemCommandSessions: {},
  delegatingSessions: {},
  routeSessions: {},
  historyHasMore: {},
  historyLoading: {},
  loadHistory: mocks.loadHistory,
  loadMoreHistory: mocks.loadMoreHistory,
  setActiveSession: mocks.setActiveSession,
};

vi.mock("@/stores/chatStore", () => ({
  useChatStore: (selector?: (state: typeof chatStoreState) => unknown) =>
    selector ? selector(chatStoreState) : chatStoreState,
}));

vi.mock("@/hooks/useChatStream", () => ({
  useChatStream: () => ({ send: vi.fn(), cancel: vi.fn() }),
}));

vi.mock("@/hooks/useAgentStream", () => ({
  useAgentStream: () => ({
    agent_id: "l1-instance",
    processing: true,
    segments: [{ type: "content", text: "channel live output" }],
    iteration: 1,
  }),
}));

vi.mock("@/stores/agentStore", () => ({
  useAgentStore: (selector: (state: unknown) => unknown) =>
    selector({
      agents: {
        agents: [
          { id: "l1-agent", instance_id: "l1-instance", state: "processing" },
        ],
      },
    }),
}));

vi.mock("@/stores/runtimeStore", () => ({
  useRuntimeStore: (selector: (state: unknown) => unknown) =>
    selector({ sidebarCollapsed: false, status: null, connectionStatus: "connected" }),
}));

vi.mock("@/lib/api", () => ({
  getSkills: vi.fn().mockResolvedValue({ skills: [], total: 0 }),
}));

vi.mock("@/lib/i18n", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("@/lib/websocket", () => ({
  wsManager: { hasChatHandler: () => false },
}));

vi.mock("@/hooks/useStickToBottom", () => ({
  useStickToBottom: () => ({
    contentRef: { current: null },
    followOutput: vi.fn(),
    resetFollow: vi.fn(),
    detachFollow: vi.fn(),
  }),
}));

vi.mock("@/components/ChatMessage", () => ({
  ChatMessageView: ({ message }: { message: { segments: Array<{ text?: string }> } }) => (
    <div>{message.segments.map((segment) => segment.text).join("")}</div>
  ),
}));

vi.mock("@/components/ChatInput", () => ({
  ChatInput: (props: typeof mocks.chatInputProps) => {
    mocks.chatInputProps = props;
    return <div data-testid="chat-input" />;
  },
}));

vi.mock("@/components/chat/AgentWorkingIndicator", () => ({
  AgentWorkingIndicator: (props: typeof mocks.agentWorkingProps) => {
    mocks.agentWorkingProps = props;
    return <div data-testid="working-indicator" />;
  },
}));

beforeEach(() => {
  mocks.chatInputProps = {};
  mocks.agentWorkingProps = {};
  chatStoreState.streamingSessions = {};
  chatStoreState.routeSessions = {};
});

describe("AssistantPage", () => {
  it("passes L1 request metadata through the shared input and working indicator", async () => {
    chatStoreState.streamingSessions = { l1: true };
    chatStoreState.routeSessions = {
      l1: {
        requestId: "req-l1",
        modelId: "l1-model",
        taskLevel: "general",
      },
    };

    render(<AssistantPage />);

    await screen.findByTestId("working-indicator");
    expect(mocks.agentWorkingProps).toMatchObject({
      modelName: "l1-model",
      taskLevel: "general",
    });
    expect(mocks.chatInputProps).toMatchObject({
      modelName: "l1-model",
      taskLevel: "general",
    });
  });

  it("does not attach an unscoped L1 channel stream to local history", async () => {
    render(<AssistantPage />);

    await waitFor(() =>
      expect(screen.queryByText("channel live output")).not.toBeInTheDocument(),
    );
  });
});
