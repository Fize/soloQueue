import { useSimStore, Task } from '../stores/simStore'
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
        className="bg-[#241a0e] border border-[#e6b053]/25 p-3 rounded-lg shadow hover:border-[#e6b053]/50 transition-colors flex flex-col justify-between gap-2 mb-3 last:mb-0"
      >
        <div>
          {/* Label Team category */}
          <span className={`font-pixel text-[8px] px-1.5 py-0.5 border rounded leading-none inline-block mb-1.5 ${
            task.team === 'infra' ? 'bg-[#38bdf8]/10 text-[#38bdf8] border-[#38bdf8]/30' :
            task.team === 'logic' ? 'bg-[#a78bfa]/10 text-[#a78bfa] border-[#a78bfa]/30' : 'bg-[#f472b6]/10 text-[#f472b6] border-[#f472b6]/30'
          }`}>
            {task.team.toUpperCase()}
          </span>
          <h4 className="text-[16px] font-bold text-[#f6ebd3] leading-tight">
            {task.title}
          </h4>
        </div>

        <div className="flex justify-between items-center border-t border-[#e6b053]/10 pt-2 mt-1">
          <span className="font-pixel text-[9px] text-emerald-400 font-bold">💰 {task.reward}</span>
          
          {/* Action based on status */}
          {task.status === 'todo' && (
            <div className="relative group">
              {idleAgents.length > 0 ? (
                <div className="flex items-center gap-1">
                  <select 
                    onChange={(e) => handleAssign(task.id, e.target.value)}
                    defaultValue=""
                    className="font-pixel text-[8px] bg-[#1a0f08] border border-[#e6b053]/30 px-1.5 py-0.5 rounded outline-none text-[#f6ebd3] cursor-pointer hover:bg-[#241a0e] transition-colors"
                  >
                    <option value="" disabled className="bg-[#1a0f08]">RUN</option>
                    {idleAgents.map(a => (
                      <option key={a.id} value={a.id} className="bg-[#1a0f08] text-[#f6ebd3]">
                        {a.name} ({a.type})
                      </option>
                    ))}
                  </select>
                </div>
              ) : (
                <span className="font-pixel text-[8px] text-[#8c7662] italic">No idle staff</span>
              )}
            </div>
          )}

          {task.status === 'running' && assignedAgent && (
            <div className="flex flex-col items-end w-full max-w-[100px]">
              <span className="font-pixel text-[8px] text-[#8c7662] truncate max-w-full">
                {assignedAgent.name}
              </span>
              <div className="w-full bg-[#1a0f08] h-2 p-[1px] border border-[#e6b053]/15 rounded-full mt-1">
                <div className="bg-emerald-500 h-full rounded-full" style={{ width: `${task.progress}%` }} />
              </div>
            </div>
          )}

          {task.status === 'done' && (
            <span className="font-pixel text-[8px] text-[#8c7662] italic">
              Completed {task.completedAt}
            </span>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="fixed inset-0 bg-black/60 z-50 flex justify-center items-center p-4 md:p-6 backdrop-blur-sm animate-in fade-in duration-150">
      <div className="bg-[#1a1209] border border-[#e6b053]/30 w-full max-w-4xl max-h-[85vh] flex flex-col shadow-2xl rounded-xl overflow-hidden font-retro">
        {/* Title bar */}
        <div className="flex justify-between items-center border-b border-[#e6b053]/20 bg-[#1a0f08] p-4 rounded-t-xl shrink-0">
          <h2 className="font-pixel text-[12px] text-[#f6ebd3] tracking-wider leading-none">
            📋 TASK KANBAN SYSTEM
          </h2>
          <button 
            onClick={onClose}
            className="text-[#f6ebd3] hover:text-white text-[14px] font-bold px-2 py-1 border border-[#e6b053]/40 hover:bg-[#e6b053]/20 rounded transition-colors"
          >
            ✕
          </button>
        </div>

        {/* Kanban Board Layout */}
        <div className="flex-1 overflow-hidden p-4 grid grid-cols-1 md:grid-cols-3 gap-4 min-h-[300px] bg-[#1a1209]">
          {/* Column 1: TODO */}
          <div className="flex flex-col h-full bg-[#0f0a05]/60 border border-[#e6b053]/15 p-3 rounded-lg overflow-hidden">
            <h3 className="font-pixel text-[10px] text-[#f6ebd3] border-b border-[#e6b053]/20 pb-2 mb-3 flex justify-between">
              <span>BACKLOG / TODO</span>
              <span className="bg-[#e6b053]/20 text-[#e6b053] px-1.5 py-0.5 text-[8px] rounded">{todoTasks.length}</span>
            </h3>
            <div className="flex-1 overflow-y-auto pr-1">
              {todoTasks.length > 0 ? (
                todoTasks.map(t => renderCard(t))
              ) : (
                <div className="text-center text-[#8c7662] py-12 font-retro text-[14px]">
                  No pending issues.
                </div>
              )}
            </div>
          </div>

          {/* Column 2: RUNNING */}
          <div className="flex flex-col h-full bg-[#0f0a05]/60 border border-[#e6b053]/15 p-3 rounded-lg overflow-hidden">
            <h3 className="font-pixel text-[10px] text-[#f6ebd3] border-b border-[#e6b053]/20 pb-2 mb-3 flex justify-between">
              <span>ACTIVE PROGRESS</span>
              <span className="bg-[#e6b053]/20 text-[#e6b053] px-1.5 py-0.5 text-[8px] rounded">{runningTasks.length}</span>
            </h3>
            <div className="flex-1 overflow-y-auto pr-1">
              {runningTasks.length > 0 ? (
                runningTasks.map(t => renderCard(t))
              ) : (
                <div className="text-center text-[#8c7662] py-12 font-retro text-[14px]">
                  No active processes.
                </div>
              )}
            </div>
          </div>

          {/* Column 3: DONE */}
          <div className="flex flex-col h-full bg-[#0f0a05]/60 border border-[#e6b053]/15 p-3 rounded-lg overflow-hidden">
            <h3 className="font-pixel text-[10px] text-[#f6ebd3] border-b border-[#e6b053]/20 pb-2 mb-3 flex justify-between">
              <span>COMPLETED</span>
              <span className="bg-[#e6b053]/20 text-[#e6b053] px-1.5 py-0.5 text-[8px] rounded">{doneTasks.length}</span>
            </h3>
            <div className="flex-1 overflow-y-auto pr-1">
              {doneTasks.length > 0 ? (
                doneTasks.map(t => renderCard(t))
              ) : (
                <div className="text-center text-[#8c7662] py-12 font-retro text-[14px]">
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
