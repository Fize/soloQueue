import { useSimStore, Task } from '../store/simStore'
import { sounds } from '../utils/audio'

interface KanbanBoardProps {
  onClose: () => void
}

export default function KanbanBoard({ onClose }: KanbanBoardProps) {
  const { tasks, agents, assignTask } = useSimStore()

  // Filter tasks by status
  const todoTasks = tasks.filter(t => t.status === 'todo')
  const runningTasks = tasks.filter(t => t.status === 'running')
  const doneTasks = tasks.filter(t => t.status === 'done')

  // Find idle agents to assign tasks
  const idleAgents = agents.filter(a => a.status === 'idle')

  const handleAssign = (taskId: string, agentId: string) => {
    sounds.playSelect()
    assignTask(taskId, agentId)
  }

  // Draw Kanban Card
  const renderCard = (task: Task) => {
    const assignedAgent = agents.find(a => a.id === task.assignedAgentId)

    return (
      <div 
        key={task.id} 
        className="border-2 border-wood-pine bg-parchment p-3 shadow-md flex flex-col justify-between gap-2 mb-3 last:mb-0"
      >
        <div>
          {/* Label Team category */}
          <span className={`font-pixel text-[8px] px-1.5 py-0.5 border border-wood-pine leading-none inline-block mb-1.5 ${
            task.team === 'infra' ? 'bg-[#38bdf8] text-white' :
            task.team === 'logic' ? 'bg-[#a78bfa] text-white' : 'bg-[#f472b6] text-white'
          }`}>
            {task.team.toUpperCase()}
          </span>
          <h4 className="text-[18px] font-bold text-charcoal-brown leading-tight">
            {task.title}
          </h4>
        </div>

        <div className="flex justify-between items-center border-t border-parchment-dark pt-1.5 mt-1">
          <span className="font-pixel text-[9px] text-crop-green font-bold">💰 {task.reward}</span>
          
          {/* Action based on status */}
          {task.status === 'todo' && (
            <div className="relative group">
              {idleAgents.length > 0 ? (
                <div className="flex items-center gap-1">
                  <select 
                    onChange={(e) => handleAssign(task.id, e.target.value)}
                    defaultValue=""
                    className="font-pixel text-[8px] bg-parchment-dark border border-wood-pine px-1 py-0.5 outline-none text-charcoal-brown cursor-pointer"
                  >
                    <option value="" disabled>RUN</option>
                    {idleAgents.map(a => (
                      <option key={a.id} value={a.id}>
                        {a.name} ({a.type})
                      </option>
                    ))}
                  </select>
                </div>
              ) : (
                <span className="font-pixel text-[8px] text-grey-brown italic">No idle staff</span>
              )}
            </div>
          )}

          {task.status === 'running' && assignedAgent && (
            <div className="flex flex-col items-end w-full max-w-[100px]">
              <span className="font-pixel text-[8px] text-grey-brown truncate max-w-full">
                {assignedAgent.name}
              </span>
              <div className="w-full bg-charcoal-brown h-2 p-[1px] border border-wood-pine mt-0.5">
                <div className="bg-crop-green h-full" style={{ width: `${task.progress}%` }} />
              </div>
            </div>
          )}

          {task.status === 'done' && (
            <span className="font-pixel text-[8px] text-grey-brown italic">
              Completed {task.completedAt}
            </span>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="fixed inset-0 bg-black/60 z-30 flex justify-center items-center p-6 backdrop-blur-sm">
      <div className="pixel-border-wood bg-wood-oak w-full max-w-4xl max-h-[85vh] flex flex-col shadow-2xl">
        {/* Title bar */}
        <div className="flex justify-between items-center border-b-4 border-charcoal-brown bg-wood-pine p-3">
          <h2 className="font-pixel text-[12px] text-parchment tracking-wider leading-none">
            📋 TASK KANBAN SYSTEM
          </h2>
          <button 
            onClick={onClose}
            className="pixel-btn pixel-btn-red py-1 px-3 text-[10px]"
          >
            CLOSE
          </button>
        </div>

        {/* Kanban Board Layout */}
        <div className="flex-1 overflow-hidden p-4 grid grid-cols-1 md:grid-cols-3 gap-4 min-h-[300px]">
          {/* Column 1: TODO */}
          <div className="flex flex-col h-full bg-[#a25927] border-2 border-wood-pine p-3 overflow-hidden">
            <h3 className="font-pixel text-[10px] text-parchment border-b-2 border-wood-pine pb-2 mb-3 flex justify-between">
              <span>BACKLOG / TODO</span>
              <span className="bg-wood-pine px-1.5 py-0.5 text-[8px]">{todoTasks.length}</span>
            </h3>
            <div className="flex-1 overflow-y-auto pr-1">
              {todoTasks.length > 0 ? (
                todoTasks.map(t => renderCard(t))
              ) : (
                <div className="text-center text-parchment/60 py-8 font-retro text-[18px]">
                  No pending issues.
                </div>
              )}
            </div>
          </div>

          {/* Column 2: RUNNING */}
          <div className="flex flex-col h-full bg-[#a25927] border-2 border-wood-pine p-3 overflow-hidden">
            <h3 className="font-pixel text-[10px] text-parchment border-b-2 border-wood-pine pb-2 mb-3 flex justify-between">
              <span>ACTIVE PROGRESS</span>
              <span className="bg-wood-pine px-1.5 py-0.5 text-[8px]">{runningTasks.length}</span>
            </h3>
            <div className="flex-1 overflow-y-auto pr-1">
              {runningTasks.length > 0 ? (
                runningTasks.map(t => renderCard(t))
              ) : (
                <div className="text-center text-parchment/60 py-8 font-retro text-[18px]">
                  No active processes.
                </div>
              )}
            </div>
          </div>

          {/* Column 3: DONE */}
          <div className="flex flex-col h-full bg-[#a25927] border-2 border-wood-pine p-3 overflow-hidden">
            <h3 className="font-pixel text-[10px] text-parchment border-b-2 border-wood-pine pb-2 mb-3 flex justify-between">
              <span>COMPLETED</span>
              <span className="bg-wood-pine px-1.5 py-0.5 text-[8px]">{doneTasks.length}</span>
            </h3>
            <div className="flex-1 overflow-y-auto pr-1">
              {doneTasks.length > 0 ? (
                doneTasks.map(t => renderCard(t))
              ) : (
                <div className="text-center text-parchment/60 py-8 font-retro text-[18px]">
                  No completed tasks yet.
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
