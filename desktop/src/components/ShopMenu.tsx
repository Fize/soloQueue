import { useState } from 'react'
import { useSimStore, AgentType } from '../stores/simStore'
import { sounds } from '../utils/audio'

interface ShopMenuProps {
  onClose: () => void
}

export default function ShopMenu({ onClose }: ShopMenuProps) {
  const { tokens, upgrades, buyUpgrade, hireAgent, agents } = useSimStore()
  const [activeTab, setActiveTab] = useState<'upgrades' | 'hire'>('upgrades')

  // Hiring Form States
  const [hireName, setHireName] = useState('Claude-3.5')
  const [hireType, setHireType] = useState<AgentType>('L3')
  const [hireGender, setHireGender] = useState<'male' | 'female'>('female')

  const handleUpgrade = (id: string) => {
    buyUpgrade(id)
  }

  const handleHire = () => {
    if (!hireName.trim()) return
    hireAgent(hireName.trim(), hireType, hireGender)
    
    // Reset form with random agent name suggestion
    const names = hireType === 'L2' 
      ? ['DeepSeek-R1', 'GPT-4o-Leader', 'Qwen-Max-Lead', 'Gemini-Flash-Mgr'] 
      : ['Claude-3.5', 'Llama-3.3', 'Mistral-Large', 'DeepSeek-Chat', 'Phind-Code']
    setHireName(names[Math.floor(Math.random() * names.length)])
  }

  // Pre-calculate upgrade costs
  const getUpgradeCost = (id: string) => {
    const u = upgrades[id]
    if (!u) return 0
    return Math.floor(u.baseCost * Math.pow(u.costMultiplier, u.level - 1))
  }

  return (
    <div className="fixed inset-0 bg-black/50 z-50 flex justify-center items-center p-4 md:p-6 backdrop-blur-sm animate-in fade-in duration-150">
      <div className="bg-white border border-gray-200 w-full max-w-2xl max-h-[85vh] flex flex-col shadow-2xl rounded-xl overflow-hidden font-retro">
        
        {/* Title bar */}
        <div className="flex justify-between items-center border-b border-gray-200 bg-gray-50 p-4 rounded-t-xl shrink-0">
          <h2 className="font-pixel text-[12px] text-gray-800 tracking-wider leading-none">
            🛒 SOLOHUB SHOP & RECRUITMENT
          </h2>
          <div className="flex items-center gap-4">
            <span className="font-pixel text-[10px] text-emerald-600 font-bold bg-white border border-gray-200 px-2.5 py-1 rounded">
              💰 {tokens.toLocaleString()}
            </span>
            <button 
              onClick={onClose}
              className="text-gray-500 hover:text-gray-800 text-[14px] font-bold px-2 py-1 border border-gray-300 hover:bg-gray-100 rounded transition-colors"
            >
              ✕
            </button>
          </div>
        </div>

        {/* Tab Selection */}
        <div className="flex bg-gray-50 border-b border-gray-200 p-1 gap-1 shrink-0">
          <button 
            onClick={() => { sounds.playSelect(); setActiveTab('upgrades'); }}
            className={`py-2 text-[10px] flex-1 rounded font-bold transition-all ${
              activeTab === 'upgrades'
                ? 'bg-primary/10 text-primary border border-primary/20'
                : 'text-gray-400 hover:text-gray-700 hover:bg-white'
            }`}
          >
            ⚙️ UPGRADES
          </button>
          <button 
            onClick={() => { sounds.playSelect(); setActiveTab('hire'); }}
            className={`py-2 text-[10px] flex-1 rounded font-bold transition-all ${
              activeTab === 'hire'
                ? 'bg-primary/10 text-primary border border-primary/20'
                : 'text-gray-400 hover:text-gray-700 hover:bg-white'
            }`}
          >
            👥 HIRE STAFF
          </button>
        </div>

        {/* Shop Contents */}
        <div className="flex-1 overflow-y-auto p-4 bg-white min-h-[300px]">
          
          {/* TAB 1: UPGRADES */}
          {activeTab === 'upgrades' && (
            <div className="flex flex-col gap-4">
              {Object.values(upgrades).map(u => {
                const cost = getUpgradeCost(u.id)
                const isMax = u.level >= u.maxLevel
                const canAfford = tokens >= cost

                return (
                  <div 
                    key={u.id}
                    className="bg-gray-50 border border-gray-200 p-3 rounded-lg flex justify-between items-center gap-4 hover:border-primary/30 transition-colors shadow-sm"
                  >
                    <div className="flex-1">
                      <div className="flex justify-between items-baseline mb-1">
                        <h4 className="text-[16px] font-bold text-gray-800 leading-none">
                          {u.name}
                        </h4>
                        <span className="font-pixel text-[8px] text-gray-400">
                          Level {u.level}/{u.maxLevel}
                        </span>
                      </div>
                      <p className="text-[12px] text-gray-400 leading-tight">
                        {u.description}
                      </p>
                    </div>

                    <div className="shrink-0 flex flex-col items-end gap-1.5">
                      {!isMax ? (
                        <>
                          <span className={`font-pixel text-[9px] font-bold ${canAfford ? 'text-emerald-600' : 'text-red-500'}`}>
                            💰 {cost}
                          </span>
                          <button 
                            onClick={() => handleUpgrade(u.id)}
                            disabled={!canAfford}
                            className={`px-3 py-1 text-[9px] font-bold rounded border transition-colors ${
                              !canAfford 
                                ? 'bg-gray-100 border-gray-200 text-gray-400 cursor-not-allowed'
                                : 'bg-primary text-primary-foreground border-primary hover:bg-primary/95 cursor-pointer'
                            }`}
                          >
                            UPGRADE
                          </button>
                        </>
                      ) : (
                        <span className="font-pixel text-[8px] text-gray-450 font-bold italic">
                          MAX LEVEL
                        </span>
                      )}
                    </div>
                  </div>
                )
              })}
            </div>
          )}

          {/* TAB 2: HIRE STAFF */}
          {activeTab === 'hire' && (
            <div className="flex flex-col md:flex-row gap-4 h-full">
              {/* Form card */}
              <div className="bg-gray-50 border border-gray-200 p-4 rounded-lg flex-1 flex flex-col justify-between gap-4">
                <div>
                  <h4 className="text-[16px] font-bold text-gray-800 border-b border-gray-200 pb-1.5 mb-3">
                    RECRUITMENT APPLICATION
                  </h4>

                  {/* Name field */}
                  <div className="mb-3">
                    <label className="text-[12px] text-gray-450 block mb-1">Employee Model Name:</label>
                    <input 
                      type="text"
                      value={hireName}
                      onChange={(e) => setHireName(e.target.value.slice(0, 16))}
                      className="w-full bg-white border border-gray-300 rounded p-1.5 outline-none font-pixel text-[10px] text-gray-800 placeholder-gray-400 focus:border-primary transition-colors"
                    />
                  </div>

                  {/* Level Type field */}
                  <div className="mb-3">
                    <label className="text-[12px] text-gray-455 block mb-1">Role / Rank Tier:</label>
                    <div className="flex gap-2">
                      <button 
                        onClick={() => { sounds.playSelect(); setHireType('L3'); }}
                        className={`py-1.5 px-3 text-[9px] font-bold flex-1 rounded border transition-all ${
                          hireType === 'L3'
                            ? 'bg-primary/10 text-primary border-primary'
                            : 'border-gray-200 text-gray-455 hover:text-gray-700 hover:bg-white'
                        }`}
                      >
                        L3 Worker (💰300)
                      </button>
                      <button 
                        onClick={() => { sounds.playSelect(); setHireType('L2'); }}
                        className={`py-1.5 px-3 text-[9px] font-bold flex-1 rounded border transition-all ${
                          hireType === 'L2'
                            ? 'bg-primary/10 text-primary border-primary'
                            : 'border-gray-200 text-gray-455 hover:text-gray-700 hover:bg-white'
                        }`}
                      >
                        L2 Leader (💰600)
                      </button>
                    </div>
                  </div>

                  {/* Gender Selection */}
                  <div className="mb-2">
                    <label className="text-[12px] text-gray-455 block mb-1">Gender / Style variant:</label>
                    <div className="flex gap-2">
                      <button 
                        onClick={() => { sounds.playSelect(); setHireGender('female'); }}
                        className={`py-1 px-3 text-[9px] font-bold flex-1 rounded border transition-all ${
                          hireGender === 'female'
                            ? 'bg-primary/10 text-primary border-primary'
                            : 'border-gray-200 text-gray-455 hover:text-gray-700 hover:bg-white'
                        }`}
                      >
                        👩 Female Variant
                      </button>
                      <button 
                        onClick={() => { sounds.playSelect(); setHireGender('male'); }}
                        className={`py-1 px-3 text-[9px] font-bold flex-1 rounded border transition-all ${
                          hireGender === 'male'
                            ? 'bg-primary/10 text-primary border-primary'
                            : 'border-gray-200 text-gray-455 hover:text-gray-700 hover:bg-white'
                        }`}
                      >
                        👨 Male Variant
                      </button>
                    </div>
                  </div>
                </div>

                {/* Confirm Hiring */}
                <button 
                  onClick={handleHire}
                  disabled={tokens < (hireType === 'L2' ? 600 : 300) || !hireName.trim()}
                  className={`py-2 text-[11px] font-bold w-full rounded border transition-colors ${
                    tokens < (hireType === 'L2' ? 600 : 300) || !hireName.trim()
                      ? 'bg-gray-100 border-gray-200 text-gray-400 cursor-not-allowed'
                      : 'bg-primary text-primary-foreground border-primary hover:bg-primary/95 cursor-pointer'
                  }`}
                >
                  ✓ SIGN WORK CONTRACT
                </button>
              </div>

              {/* Staff Roster panel */}
              <div className="bg-gray-50 border border-gray-200 p-3 w-full md:w-60 flex flex-col rounded-lg shrink-0">
                <span className="font-pixel text-[8px] text-gray-455 border-b border-gray-200 pb-1 mb-2">
                  OFFICE ROSTER ({agents.length}/7 desks)
                </span>
                <div className="flex-1 overflow-y-auto flex flex-col gap-1.5 pr-1 max-h-[220px] md:max-h-[300px]">
                  {agents.map(a => (
                    <div key={a.id} className="border border-gray-200 bg-white p-2 rounded flex justify-between items-center text-[12px]">
                      <div>
                        <div className="font-bold text-gray-800 leading-none">{a.name}</div>
                        <div className="font-pixel text-[7px] text-gray-400 mt-1">{a.type} | {a.workstationId}</div>
                      </div>
                      <span className={`text-[9px] px-1 py-0.5 rounded leading-none text-white font-bold ${
                        a.status === 'working' ? 'bg-[#7ca84c]' :
                        a.status === 'error' ? 'bg-[#d83838]' : 'bg-gray-400'
                      }`}>
                        {a.status.slice(0, 4).toUpperCase()}
                      </span>
                    </div>
                  ))}
                </div>
              </div>

            </div>
          )}

        </div>
      </div>
    </div>
  )
}