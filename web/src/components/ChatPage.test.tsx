import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ChatPage } from "./ChatPage";

const mocks = vi.hoisted(() => ({
  loadHistory: vi.fn(),
  loadMoreHistory: vi.fn().mockResolvedValue(undefined),
  setActiveSession: vi.fn(),
  createL2Session: vi.fn(),
  deleteL2Session: vi.fn(),
  fetchLiveAgents: vi.fn(),
  fetchTeams: vi.fn(),
  chatStreamSend: vi.fn().mockResolvedValue(undefined),
  chatInputProps: {} as {
    onSend?: (
      text: string,
      files?: { name: string; path: string }[],
      group?: string,
      projectPath?: string,
      selectedElement?: unknown,
    ) => Promise<void>;
  },
  setInspectorPanelWidth: vi.fn(),
  runtimeStoreState: {
    connectionStatus: "connected",
    sidebarCollapsed: false,
    isDesignMode: false,
    status: null,
    setInspectorPanelWidth: vi.fn(),
    setDesignMode: vi.fn(),
  },
  designPanelProps: {} as {
    onResizeStart?: unknown;
    onDesignContextChange?: (ctx: { activeDesignFile?: string; hasDrawings: boolean }) => void;
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
  useChatStream: () => ({ send: mocks.chatStreamSend, cancel: vi.fn() }),
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

vi.mock("@/components/ChatInput", () => ({
  ChatInput: (props: typeof mocks.chatInputProps) => {
    mocks.chatInputProps = props;
    return <div data-testid="chat-input" />;
  },
}));
vi.mock("@/components/chat/AgentWorkingIndicator", () => ({ AgentWorkingIndicator: () => <div /> }));
vi.mock("./chat/SessionInspectorPanel", () => ({ SessionInspectorPanel: () => <div /> }));
vi.mock("@/components/ChatDesignPanel", () => ({
  ChatDesignPanel: (props: {
    isDesignMode: boolean;
    onResizeStart?: unknown;
    onDesignContextChange?: (ctx: { activeDesignFile?: string; hasDrawings: boolean }) => void;
  }) => {
    mocks.designPanelProps = props;
    return <div data-testid="design-panel" data-design-mode={String(props.isDesignMode)} />;
  },
}));

function setViewport(width: number, height: number) {
  Object.defineProperty(window, "innerWidth", { configurable: true, value: width });
  Object.defineProperty(window, "innerHeight", { configurable: true, value: height });
  window.dispatchEvent(new Event("resize"));
}

beforeEach(() => {
  mocks.runtimeStoreState.isDesignMode = false;
  mocks.runtimeStoreState.setDesignMode = vi.fn((enabled: boolean) => {
    mocks.runtimeStoreState.isDesignMode = enabled;
  });
  mocks.chatStreamSend.mockClear();
  mocks.chatInputProps = {};
  mocks.designPanelProps = {};
  setViewport(1024, 768);
});

describe("ChatPage", () => {
  it("renders an L2 channel stream without a Desktop request ID", async () => {
    render(<ChatPage />);

    await waitFor(() =>
      expect(screen.getByText("channel L2 live output")).toBeInTheDocument(),
    );
  });

  it("exits persisted Design Mode on Phone without mounting the design panel", async () => {
    mocks.runtimeStoreState.isDesignMode = true;
    setViewport(719, 800);

    render(<ChatPage />);

    await waitFor(() => expect(mocks.runtimeStoreState.setDesignMode).toHaveBeenCalledWith(false));
    expect(screen.queryByTestId("design-panel")).not.toBeInTheDocument();
  });

  it("renders Pad portrait as a single-pane design layout", async () => {
    mocks.runtimeStoreState.isDesignMode = true;
    setViewport(999, 800);

    render(<ChatPage />);

    expect(await screen.findByTestId("design-panel")).toBeInTheDocument();
    expect(screen.getByTestId("design-panel").parentElement?.parentElement).toHaveClass("flex-col");
    expect(screen.getByTestId("design-panel")).toHaveAttribute("data-design-mode", "true");
    expect(mocks.designPanelProps.onResizeStart).toBeUndefined();
  });

  it("renders Pad landscape as a split-pane design layout", async () => {
    mocks.runtimeStoreState.isDesignMode = true;
    setViewport(1000, 800);

    render(<ChatPage />);

    expect(await screen.findByTestId("design-panel")).toBeInTheDocument();
    expect(screen.getByTestId("design-panel").closest("[data-design-layout]")).toHaveAttribute(
      "data-design-layout",
      "split",
    );
    expect(mocks.designPanelProps.onResizeStart).toEqual(expect.any(Function));
  });

  it("clears persisted Design Mode across a mounted Phone downgrade without resetting design data or re-entering", async () => {
    localStorage.setItem("design-data-sentinel", "keep-me");
    mocks.runtimeStoreState.isDesignMode = true;
    setViewport(1000, 800);

    render(<ChatPage />);

    const panel = await screen.findByTestId("design-panel");
    mocks.designPanelProps.onDesignContextChange?.({
      activeDesignFile: "/workspace/design.html",
      hasDrawings: true,
    });

    setViewport(1000, 599);
    await waitFor(() => {
      expect(screen.queryByTestId("design-panel")).not.toBeInTheDocument();
      expect(mocks.runtimeStoreState.setDesignMode).toHaveBeenCalledWith(false);
    });
    expect(panel).not.toBeInTheDocument();
    expect(localStorage.getItem("design-data-sentinel")).toBe("keep-me");

    // The exact height boundary is capable again, but the user must opt back in.
    setViewport(1000, 600);
    expect(screen.queryByTestId("design-panel")).not.toBeInTheDocument();

    // Orientation changes use the same capability path and must not re-enter.
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 720 });
    Object.defineProperty(window, "innerHeight", { configurable: true, value: 600 });
    window.dispatchEvent(new Event("orientationchange"));
    expect(screen.queryByTestId("design-panel")).not.toBeInTheDocument();

    await mocks.chatInputProps.onSend?.("ordinary message", undefined, undefined, undefined, {
      file_path: "/workspace/stale.html",
      selector: "#stale",
    });
    expect(mocks.chatStreamSend).toHaveBeenCalledWith(
      "ordinary message",
      undefined,
      "channel-l2",
      undefined,
      undefined,
      false,
    );
  });
});
