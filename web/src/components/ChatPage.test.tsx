import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ChatPage } from "./ChatPage";

const mocks = vi.hoisted(() => ({
  loadHistory: vi.fn(),
  loadMoreHistory: vi.fn().mockResolvedValue(undefined),
  setActiveSession: vi.fn(),
  createL2Session: vi.fn(),
  deleteL2Session: vi.fn(),
  fetchLiveAgents: vi.fn(),
  fetchTeams: vi.fn(),
  setInspectorPanelWidth: vi.fn(),
  runtimeStoreState: {
    connectionStatus: "connected",
    sidebarCollapsed: false,
    isDesignMode: false,
    status: null,
    setInspectorPanelWidth: vi.fn(),
  },
}));

const chatStoreState = {
  activeSessionId: "channel-l2",
  messages: { "channel-l2": [] },
  streamingSessions: {},
  systemCommandSessions: {},
  delegatingSessions: {},
  routeSessions: {},
  sessions: [
    {
      id: "channel-l2",
      group: "team",
      agent_instance_id: "l2-instance",
      agent_name: "Channel L2",
      project_path: "",
    },
  ],
  sessionsLoading: false,
  sessionsLoaded: true,
  historyHasMore: {},
  historyLoading: {},
  loadHistory: mocks.loadHistory,
  loadMoreHistory: mocks.loadMoreHistory,
  setActiveSession: mocks.setActiveSession,
  createL2Session: mocks.createL2Session,
  deleteL2Session: mocks.deleteL2Session,
};

vi.mock("react-router-dom", () => ({
  useParams: () => ({ sessionId: "channel-l2" }),
  useNavigate: () => vi.fn(),
}));

vi.mock("@/stores/chatStore", () => ({
  useChatStore: (selector: (state: typeof chatStoreState) => unknown) =>
    selector(chatStoreState),
}));

vi.mock("@/stores/agentStore", () => ({
  useAgentStore: (selector: (state: unknown) => unknown) =>
    selector({
      agents: {
        agents: [
          {
            id: "leader",
            instance_id: "l2-instance",
            state: "processing",
            group: "team",
            is_leader: true,
          },
        ],
      },
      teams: { teams: [{ name: "team", agents: [{ id: "leader", is_leader: true }] }] },
      fetchLiveAgents: mocks.fetchLiveAgents,
      fetchTeams: mocks.fetchTeams,
    }),
}));

vi.mock("@/stores/runtimeStore", () => ({
  useRuntimeStore: Object.assign(
    (selector: (state: typeof mocks.runtimeStoreState) => unknown) =>
      selector(mocks.runtimeStoreState),
    { getState: () => mocks.runtimeStoreState },
  ),
}));

vi.mock("@/stores/connectionStore", () => ({
  useConnectionStore: (selector: (state: { backendStatus: { running: boolean } }) => unknown) =>
    selector({ backendStatus: { running: true } }),
}));

vi.mock("@/hooks/useChatStream", () => ({
  useChatStream: () => ({ send: vi.fn(), cancel: vi.fn() }),
}));

vi.mock("@/hooks/useAgentStream", () => ({
  useAgentStream: () => ({
    agent_id: "l2-instance",
    processing: true,
    segments: [{ type: "content", text: "channel L2 live output" }],
    iteration: 1,
  }),
}));

vi.mock("@/hooks/useResizablePanes", () => ({
  useResizablePanes: () => ({
    panelWidth: 0,
    isResizing: false,
    splitContainerRef: { current: null },
    handleResizeStart: vi.fn(),
    containerWidth: 1024,
  }),
}));

vi.mock("@/hooks/useStickToBottom", () => ({
  useStickToBottom: () => ({
    contentRef: { current: null },
    followOutput: vi.fn(),
    resetFollow: vi.fn(),
    detachFollow: vi.fn(),
    syncFollowState: vi.fn(),
  }),
}));

vi.mock("@/lib/api", () => ({
  listL2Groups: vi.fn().mockResolvedValue(["team"]),
  listProjects: vi.fn().mockResolvedValue([]),
  getTeams: vi.fn().mockResolvedValue({ teams: [] }),
  getSkills: vi.fn().mockResolvedValue({ skills: [], total: 0 }),
  listAgents: vi.fn().mockResolvedValue([]),
}));

vi.mock("@/lib/i18n", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("@/lib/websocket", () => ({
  wsManager: { hasChatHandler: () => false },
}));

vi.mock("@/components/ChatMessage", () => ({
  ChatMessageView: ({ message }: { message: { segments: Array<{ text?: string }> } }) => (
    <div>{message.segments.map((segment) => segment.text).join("")}</div>
  ),
}));

vi.mock("@/components/ChatInput", () => ({ ChatInput: () => <div /> }));
vi.mock("@/components/chat/AgentWorkingIndicator", () => ({ AgentWorkingIndicator: () => <div /> }));
vi.mock("./chat/SessionInspectorPanel", () => ({ SessionInspectorPanel: () => <div /> }));
vi.mock("@/components/ChatDesignPanel", () => ({ ChatDesignPanel: () => <div /> }));

describe("ChatPage", () => {
  it("renders an L2 channel stream without a Desktop request ID", async () => {
    render(<ChatPage />);

    await waitFor(() =>
      expect(screen.getByText("channel L2 live output")).toBeInTheDocument(),
    );
  });
});
