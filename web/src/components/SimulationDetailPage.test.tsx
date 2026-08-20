import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { SimulationDetailPage } from './SimulationDetailPage'

const mocks = vi.hoisted(() => ({
  handlers: new Map<string, (payload: unknown) => void>(),
  navigate: vi.fn(),
  getSimulation: vi.fn(),
  getSimulationEnvironment: vi.fn().mockResolvedValue({ world_state: {} }),
  listModels: vi.fn().mockResolvedValue([]),
  listProviders: vi.fn().mockResolvedValue([]),
}))

vi.mock('react-router-dom', () => ({
  useParams: () => ({ id: 'simulation-1' }),
  useNavigate: () => mocks.navigate,
}))

vi.mock('@/stores/runtimeStore', () => ({
  useRuntimeStore: (selector: (state: { sidebarCollapsed: boolean }) => unknown) =>
    selector({ sidebarCollapsed: false }),
}))

vi.mock('@/lib/websocket', () => ({
  wsManager: {
    subscribe: (type: string, handler: (payload: unknown) => void) => {
      mocks.handlers.set(type, handler)
      return vi.fn()
    },
  },
}))

vi.mock('@/lib/api', () => ({
  askSimulationAgent: vi.fn(),
  controlSimulation: vi.fn(),
  deleteSimulation: vi.fn(),
  getSimulation: mocks.getSimulation,
  getSimulationEnvironment: mocks.getSimulationEnvironment,
  listModels: mocks.listModels,
  listProviders: mocks.listProviders,
  updateSimulation: vi.fn(),
}))

vi.mock('@/lib/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('./SimulationGraph', () => ({ SimulationGraph: () => <div>graph</div> }))
vi.mock('./AgentDetailPanel', () => ({ AgentDetailPanel: () => <div>agent detail</div> }))
vi.mock('./SimulationReportModal', () => ({ SimulationReportModal: () => null }))
vi.mock('./SimulationConfigEditor', () => ({ SimulationConfigEditor: () => null }))
vi.mock('./SimulationForkDialog', () => ({ SimulationForkDialog: () => null }))

function setupSimulationTest() {
  const scrollTo = vi.fn()
  const scrollIntoView = vi.fn()
  HTMLElement.prototype.scrollTo = scrollTo
  HTMLElement.prototype.scrollIntoView = scrollIntoView
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as typeof ResizeObserver
  mocks.getSimulation.mockResolvedValue({
    run_id: 'simulation-1',
    status: 'running',
    current_round: 1,
    config: {
      id: 'simulation-1',
      topic: 'Test simulation',
      personas: [{ id: 'agent-1', name: 'Agent One', role: 'Tester' }],
    },
    rounds: [
      {
        messages: [
          {
            agent_id: 'agent-1',
            agent_name: 'Agent One',
            content: 'first message',
            round: 1,
            seq_num: 1,
          },
        ],
      },
    ],
    relationships: [],
  })
  return { scrollTo, scrollIntoView }
}

function emitSimulationMessage(seqNum: number, content: string) {
  act(() => {
    mocks.handlers.get('simulation_event')?.({
      simulation_id: 'simulation-1',
      type: 'agent_message',
      round: 1,
      data: {
        agent_id: 'agent-1',
        agent_name: 'Agent One',
        content,
        round: 1,
        seq_num: seqNum,
      },
    })
  })
}

describe('SimulationDetailPage', () => {
  const originalResizeObserver = globalThis.ResizeObserver
  const originalScrollIntoView = HTMLElement.prototype.scrollIntoView
  const originalScrollTo = HTMLElement.prototype.scrollTo

  afterEach(() => {
    globalThis.ResizeObserver = originalResizeObserver
    HTMLElement.prototype.scrollIntoView = originalScrollIntoView
    HTMLElement.prototype.scrollTo = originalScrollTo
    mocks.handlers.clear()
  })

  it('scrolls only the simulation message viewport when a message arrives', async () => {
    const { scrollTo, scrollIntoView } = setupSimulationTest()

    render(<SimulationDetailPage />)

    expect(await screen.findByText('first message')).toBeInTheDocument()
    const viewport = screen.getByTestId('simulation-message-viewport')
    Object.defineProperty(viewport, 'scrollHeight', { configurable: true, value: 400 })

    emitSimulationMessage(2, 'second message')

    expect(await screen.findByText('second message')).toBeInTheDocument()
    await waitFor(() =>
      expect(scrollTo).toHaveBeenLastCalledWith({ top: 400, behavior: 'auto' })
    )
    expect(scrollIntoView).not.toHaveBeenCalled()
  })

  it('does not follow a new message after the user starts scrolling up', async () => {
    const { scrollTo } = setupSimulationTest()

    render(<SimulationDetailPage />)

    expect(await screen.findByText('first message')).toBeInTheDocument()
    const viewport = screen.getByTestId('simulation-message-viewport')
    Object.defineProperties(viewport, {
      scrollHeight: { configurable: true, value: 400 },
      clientHeight: { configurable: true, value: 100 },
      scrollTop: { configurable: true, writable: true, value: 0 },
    })
    fireEvent.wheel(viewport)
    const callsBeforeNewMessage = scrollTo.mock.calls.length

    emitSimulationMessage(2, 'second message')

    expect(await screen.findByText('second message')).toBeInTheDocument()
    expect(scrollTo).toHaveBeenCalledTimes(callsBeforeNewMessage)
  })

  it('does not follow a new message after the user starts a touch interaction', async () => {
    const { scrollTo } = setupSimulationTest()

    render(<SimulationDetailPage />)

    expect(await screen.findByText('first message')).toBeInTheDocument()
    const viewport = screen.getByTestId('simulation-message-viewport')
    fireEvent.touchStart(viewport)
    const callsBeforeNewMessage = scrollTo.mock.calls.length

    emitSimulationMessage(2, 'second message')

    expect(await screen.findByText('second message')).toBeInTheDocument()
    expect(scrollTo).toHaveBeenCalledTimes(callsBeforeNewMessage)
  })

  it('resumes following after the user returns near the bottom', async () => {
    const { scrollTo } = setupSimulationTest()

    render(<SimulationDetailPage />)

    expect(await screen.findByText('first message')).toBeInTheDocument()
    const viewport = screen.getByTestId('simulation-message-viewport')
    Object.defineProperties(viewport, {
      scrollHeight: { configurable: true, value: 400 },
      clientHeight: { configurable: true, value: 100 },
      scrollTop: { configurable: true, writable: true, value: 0 },
    })
    fireEvent.wheel(viewport)
    viewport.scrollTop = 270
    fireEvent.scroll(viewport)

    emitSimulationMessage(2, 'second message')

    expect(await screen.findByText('second message')).toBeInTheDocument()
    await waitFor(() =>
      expect(scrollTo).toHaveBeenLastCalledWith({ top: 400, behavior: 'auto' })
    )
  })
})
