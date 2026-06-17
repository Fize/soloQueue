import { useMemo, useState, useRef, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAgentStore } from '@/stores/agentStore'
import { useChatStore } from '@/stores/chatStore'
import { Activity, Zap, Loader2, Maximize2 } from 'lucide-react'

export function GlobalAgentStatusBar() {
  const navigate = useNavigate()
  const agentsData = useAgentStore((state) => state.agents)
  const agents = agentsData?.agents || []
  const supervisors = agentsData?.supervisors || []
  const sessions = useChatStore((state) => state.sessions)
  const [isOpen, setIsOpen] = useState(false)

  const activeAgents = useMemo(() => {
    return agents.filter((a) => a.state === 'processing')
  }, [agents])

  const popoverRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (popoverRef.current && !popoverRef.current.contains(event.target as Node)) {
        setIsOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  if (activeAgents.length === 0) {
    return null
  }

  const handleNavigateToSession = async (agentInstanceId: string) => {
    const findSession = (sessList: typeof sessions) => {
      let session = sessList.find((s) => s.agent_instance_id === agentInstanceId)
      if (!session && supervisors) {
        const sv = supervisors.find((s) => s.children_ids.includes(agentInstanceId))
        if (sv) {
          session = sessList.find((s) => s.agent_instance_id === sv.leader_id)
        }
      }
      return session
    }

    let session = findSession(sessions)

    if (!session) {
      // Session might have been activated after our last fetch. Refetch and try again.
      await useChatStore.getState().loadSessions()
      const updatedSessions = useChatStore.getState().sessions
      session = findSession(updatedSessions)
    }

    if (session) {
      navigate(`/chat/${session.id}`)
      setIsOpen(false)
    } else {
      // Fallback: If no session found but it's an L1 session, it's always "l1"
      const isL1 = agents.find((a) => a.instance_id === agentInstanceId)?.group === 'L1'
      if (isL1 || activeAgents.length === 1) {
        navigate('/chat/l1')
        setIsOpen(false)
      }
    }
  }

  return (
    <div className="absolute top-4 left-1/2 -translate-x-1/2 z-50" ref={popoverRef}>
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="flex items-center gap-2 px-4 py-2 bg-primary text-primary-foreground shadow-lg rounded-full text-sm font-medium hover:bg-primary/90 transition-all shadow-primary/20 border border-primary/20 animate-in slide-in-from-top-4 fade-in"
      >
        <Zap className="w-4 h-4 fill-current" />
        <span>
          {activeAgents.length} Active {activeAgents.length === 1 ? 'Agent' : 'Agents'}
        </span>
        <span className="flex h-2 w-2 ml-1 relative">
          <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-white opacity-75"></span>
          <span className="relative inline-flex rounded-full h-2 w-2 bg-white"></span>
        </span>
      </button>

      {isOpen && (
        <div className="absolute top-full left-1/2 -translate-x-1/2 mt-3 w-80 shadow-2xl rounded-xl border border-border bg-card/95 backdrop-blur-md animate-in slide-in-from-top-2 fade-in">
          <div className="px-4 py-3 border-b border-border bg-muted/30 rounded-t-xl">
            <h3 className="font-semibold text-sm flex items-center gap-2">
              <Activity className="w-4 h-4 text-primary" />
              Running Tasks
            </h3>
          </div>
          <div className="max-h-[60vh] overflow-y-auto p-2 flex flex-col gap-1">
            {activeAgents.map((agent) => (
              <div
                key={agent.instance_id}
                className="group flex flex-col p-3 rounded-lg hover:bg-muted/50 border border-transparent hover:border-border transition-colors cursor-default"
              >
                <div className="flex items-center justify-between mb-1">
                  <div className="flex items-center gap-2 font-medium text-sm text-foreground">
                    <span className="text-xl leading-none">
                      {agent.name.includes('赵') || agent.name.includes('L1') ? '🤖' : '🧑‍💻'}
                    </span>
                    <span className="truncate max-w-[150px]">{agent.name}</span>
                  </div>
                  <button
                    onClick={() => handleNavigateToSession(agent.instance_id)}
                    className="opacity-0 group-hover:opacity-100 p-1.5 bg-primary/10 hover:bg-primary/20 text-primary rounded-md transition-all flex items-center gap-1 text-xs font-semibold cursor-pointer"
                    title="Go to Session"
                  >
                    <Maximize2 className="w-3 h-3" />
                    Session
                  </button>
                </div>

                <div className="flex items-center gap-2 text-xs text-muted-foreground ml-7">
                  <Loader2 className="w-3 h-3 animate-spin text-blue-500" />
                  <span>Processing</span>
                  {agent.group && agent.group !== 'L1' && (
                    <>
                      <span className="text-border/50">•</span>
                      <span className="truncate max-w-[120px]">{agent.group}</span>
                    </>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
