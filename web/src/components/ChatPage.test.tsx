import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ChatPage } from "./ChatPage";
import { useInspectorStateStore } from "@/stores/inspectorStateStore";

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
    modelName?: string;
    taskLevel?: string;
  },
  agentWorkingProps: {} as { modelName?: string; taskLevel?: string },
  setInspectorPanelWidth: vi.fn(),
  resizablePaneArgs: {} as {
    inspectorWidth?: number;
    onInspectorWidthChange?: (width: number) => void;
  },
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
  inspectorPanelProps: {} as {
    inspectorTab?: 'files' | 'changes' | 'plan';
    selectedPlan?: string;
    onSelectedPlanChange?: (path: string) => void;
  },
}));

let routeSessionId = "channel-l2";

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
  useParams: () => ({ sessionId: routeSessionId }),
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
  useResizablePanes: (
    _isDesignMode: boolean,
    _activeSessionId: string | null,
    inspectorWidth?: number,
    onInspectorWidthChange?: (width: number) => void,
  ) => {
    mocks.resizablePaneArgs = { inspectorWidth, onInspectorWidthChange };
    return {
      panelWidth: inspectorWidth ?? 0,
      isResizing: false,
      splitContainerRef: { current: null },
      handleResizeStart: vi.fn(),
      containerWidth: 1024,
    };
  },
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
vi.mock("@/components/chat/AgentWorkingIndicator", () => ({
  AgentWorkingIndicator: (props: typeof mocks.agentWorkingProps) => {
    mocks.agentWorkingProps = props;
    return <div data-testid="working-indicator" />;
  },
}));
vi.mock("./chat/SessionInspectorPanel", () => ({
  SessionInspectorPanel: (props: typeof mocks.inspectorPanelProps) => {
    mocks.inspectorPanelProps = props;
    return (
      <div data-testid="inspector-panel" data-tab={props.inspectorTab} data-plan={props.selectedPlan}>
        <button onClick={() => props.onSelectedPlanChange?.('/plans/a.md')}>select plan a</button>
      </div>
    );
  },
}));
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
  localStorage.clear();
  useInspectorStateStore.setState({ sessions: {} });
  routeSessionId = "channel-l2";
  mocks.runtimeStoreState.isDesignMode = false;
  mocks.runtimeStoreState.setDesignMode = vi.fn((enabled: boolean) => {
    mocks.runtimeStoreState.isDesignMode = enabled;
  });
  mocks.chatStreamSend.mockClear();
  mocks.chatInputProps = {};
  mocks.agentWorkingProps = {};
  mocks.designPanelProps = {};
  mocks.inspectorPanelProps = {};
  mocks.resizablePaneArgs = {};
  chatStoreState.activeSessionId = "channel-l2";
  chatStoreState.sessionsLoading = false;
  chatStoreState.sessionsLoaded = true;
  chatStoreState.sessions = [
    {
      id: "channel-l2",
      group: "team",
      agent_instance_id: "l2-instance",
      agent_name: "Channel L2",
      project_path: "",
      plans: ['/plans/a.md', '/plans/b.md'],
    },
    {
      id: "channel-other",
      group: "team",
      agent_instance_id: "l2-other",
      agent_name: "Channel Other",
      project_path: "",
      plans: ['/plans/other.md'],
    },
  ];
  chatStoreState.messages["channel-l2"] = [];
  chatStoreState.streamingSessions = {};
  chatStoreState.routeSessions = {};
  setViewport(1024, 768);
});

describe("ChatPage", () => {
  it("restores inspector open, tab, and selected Plan snapshots per session", async () => {
    let view = render(<ChatPage />);

    fireEvent.click(screen.getByTitle('Show Task Status Panel'));
    fireEvent.click(screen.getByRole('button', { name: 'Plan' }));
    fireEvent.click(screen.getByRole('button', { name: 'select plan a' }));
    expect(screen.getByTestId('inspector-panel')).toHaveAttribute('data-tab', 'plan');
    expect(screen.getByTestId('inspector-panel')).toHaveAttribute('data-plan', '/plans/a.md');

    routeSessionId = 'channel-other';
    chatStoreState.activeSessionId = 'channel-other';
    vi.useFakeTimers();
    view.rerender(<ChatPage />);
    act(() => {
      vi.advanceTimersByTime(400);
    });
    vi.useRealTimers();
    expect(screen.queryByTestId('inspector-panel')).not.toBeInTheDocument();

    fireEvent.click(screen.getByTitle('Show Task Status Panel'));
    fireEvent.click(screen.getByRole('button', { name: 'Changes' }));
    expect(screen.getByTestId('inspector-panel')).toHaveAttribute('data-tab', 'changes');

    routeSessionId = 'channel-l2';
    chatStoreState.activeSessionId = 'channel-l2';
    view.rerender(<ChatPage />);
    expect(screen.getByTestId('inspector-panel')).toHaveAttribute('data-tab', 'plan');
    expect(screen.getByTestId('inspector-panel')).toHaveAttribute('data-plan', '/plans/a.md');

    view.unmount();
    view = render(<ChatPage />);
    expect(screen.getByTestId('inspector-panel')).toHaveAttribute('data-tab', 'plan');
    expect(screen.getByTestId('inspector-panel')).toHaveAttribute('data-plan', '/plans/a.md');

    routeSessionId = 'channel-other';
    chatStoreState.activeSessionId = 'channel-other';
    view.rerender(<ChatPage />);
    expect(screen.getByTestId('inspector-panel')).toHaveAttribute('data-tab', 'changes');
  });

  it("restores and updates panel width for the active session", () => {
    useInspectorStateStore.getState().updateSession('channel-l2', {
      open: true,
      panelWidth: 444,
    });

    render(<ChatPage />);

    expect(mocks.resizablePaneArgs.inspectorWidth).toBe(444);
    mocks.resizablePaneArgs.onInspectorWidthChange?.(512);
    expect(useInspectorStateStore.getState().sessions['channel-l2']?.panelWidth).toBe(512);
  });

  it("clamps restored panel width against the live split container", async () => {
    const { clampInspectorWidthToContainer } = await vi.importActual<
      typeof import('@/hooks/useResizablePanes')
    >('@/hooks/useResizablePanes');

    expect(clampInspectorWidthToContainer(1_200, 800)).toBe(480);
    expect(clampInspectorWidthToContainer(0, 800)).toBe(200);
  });

  it("invalidates stale file state when the session project changes while closed", async () => {
    useInspectorStateStore.getState().updateSession('channel-l2', {
      projectPath: '/work/old',
      expandedDirectories: ['src'],
      selectedFile: 'src/old.ts',
    });
    chatStoreState.sessions[0].project_path = '/work/new';

    render(<ChatPage />);

    await waitFor(() => {
      expect(useInspectorStateStore.getState().sessions['channel-l2']).toMatchObject({
        projectPath: '/work/new',
        expandedDirectories: [],
        selectedFile: null,
      });
    });
  });

  it("does not overwrite hydrated Inspector state before delayed session metadata loads", async () => {
    useInspectorStateStore.getState().updateSession('channel-l2', {
      open: true,
      tab: 'plan',
      selectedPlan: '/plans/a.md',
      projectPath: '/work/project',
      expandedDirectories: ['src'],
      selectedFile: 'src/main.ts',
    });
    chatStoreState.sessions = [];
    chatStoreState.sessionsLoading = true;
    chatStoreState.sessionsLoaded = false;

    const view = render(<ChatPage />);

    expect(useInspectorStateStore.getState().sessions['channel-l2']).toMatchObject({
      tab: 'plan',
      selectedPlan: '/plans/a.md',
      projectPath: '/work/project',
      expandedDirectories: ['src'],
      selectedFile: 'src/main.ts',
    });

    chatStoreState.sessions = [{
      id: 'channel-l2',
      group: 'team',
      agent_instance_id: 'l2-instance',
      agent_name: 'Channel L2',
      project_path: '/work/project',
      plans: ['/plans/a.md'],
    }];
    chatStoreState.sessionsLoading = false;
    chatStoreState.sessionsLoaded = true;
    view.rerender(<ChatPage />);

    await waitFor(() => {
      expect(useInspectorStateStore.getState().sessions['channel-l2']).toMatchObject({
        tab: 'plan',
        selectedPlan: '/plans/a.md',
        projectPath: '/work/project',
        expandedDirectories: ['src'],
        selectedFile: 'src/main.ts',
      });
    });
  });

  it("does not copy the previous session project while target metadata is delayed", async () => {
    chatStoreState.sessions[0].project_path = '/work/first';
    useInspectorStateStore.getState().updateSession('channel-other', {
      projectPath: '/work/second',
      expandedDirectories: ['src'],
      selectedFile: 'src/second.ts',
    });
    const view = render(<ChatPage />);
    await waitFor(() => {
      expect(useInspectorStateStore.getState().sessions['channel-l2']?.projectPath).toBe(
        '/work/first',
      );
    });

    routeSessionId = 'channel-other';
    chatStoreState.activeSessionId = 'channel-other';
    chatStoreState.sessions = chatStoreState.sessions.filter(
      (session) => session.id !== 'channel-other',
    );
    chatStoreState.sessionsLoading = true;
    chatStoreState.sessionsLoaded = false;
    view.rerender(<ChatPage />);

    expect(useInspectorStateStore.getState().sessions['channel-other']).toMatchObject({
      projectPath: '/work/second',
      expandedDirectories: ['src'],
      selectedFile: 'src/second.ts',
    });

    chatStoreState.sessions.push({
      id: 'channel-other',
      group: 'team',
      agent_instance_id: 'l2-other',
      agent_name: 'Channel Other',
      project_path: '',
      plans: ['/plans/other.md'],
    });
    chatStoreState.sessionsLoading = false;
    chatStoreState.sessionsLoaded = true;
    view.rerender(<ChatPage />);

    await waitFor(() => {
      expect(useInspectorStateStore.getState().sessions['channel-other']).toMatchObject({
        projectPath: '',
        expandedDirectories: [],
        selectedFile: null,
      });
    });
  });

  it("passes Team request metadata through the shared input and working indicator", async () => {
    chatStoreState.messages["channel-l2"] = [{
      id: "user-1",
      role: "user",
      timestamp: "2026-10-13T15:42:00+08:00",
      segments: [{ type: "content", text: "current prompt" }],
    }];
    chatStoreState.streamingSessions = { "channel-l2": true };
    chatStoreState.routeSessions = {
      "channel-l2": {
        requestId: "req-team",
        modelId: "team-model",
        taskLevel: "engineering",
      },
    };

    render(<ChatPage />);

    await screen.findByTestId("working-indicator");
    expect(mocks.agentWorkingProps).toMatchObject({
      modelName: "team-model",
      taskLevel: "engineering",
    });
    expect(mocks.chatInputProps).toMatchObject({
      modelName: "team-model",
      taskLevel: "engineering",
    });
  });

  it("does not attach an unscoped L2 channel stream to local history", async () => {
    render(<ChatPage />);

    await waitFor(() =>
      expect(screen.queryByText("channel L2 live output")).not.toBeInTheDocument(),
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
