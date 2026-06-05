import { useState } from 'react'
import { useSimStore, AgentType } from '../store/simStore'
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
    <div className="fixed inset-0 bg-black/60 z-30 flex justify-center items-center p-6 backdrop-blur-sm">
      <div className="pixel-border-wood bg-wood-oak w-full max-w-2xl max-h-[85vh] flex flex-col shadow-2xl">
        
        {/* Title bar */}
        <div className="flex justify-between items-center border-b-4 border-charcoal-brown bg-wood-pine p-3">
          <h2 className="font-pixel text-[12px] text-parchment tracking-wider leading-none">
            🛒 SOLOHUB SHOP & RECRUITMENT
          </h2>
          <div className="flex items-center gap-4">
            <span className="font-pixel text-[11px] text-crop-green font-bold bg-parchment border border-wood-pine px-2 py-1">
              💰 {tokens}
            </span>
            <button 
              onClick={onClose}
              className="pixel-btn pixel-btn-red py-1 px-3 text-[10px]"
            >
              CLOSE
            </button>
          </div>
        </div>

        {/* Tab Selection */}
        <div className="flex bg-wood-pine/30 border-b-2 border-charcoal-brown p-1 gap-1">
          <button 
            onClick={() => { sounds.playSelect(); setActiveTab('upgrades'); }}
            className={`pixel-btn py-2 text-[10px] flex-1 ${activeTab === 'upgrades' ? 'pixel-btn-green' : ''}`}
          >
            ⚙️ UPGRADES (升级)
          </button>
          <button 
            onClick={() => { sounds.playSelect(); setActiveTab('hire'); }}
            className={`pixel-btn py-2 text-[10px] flex-1 ${activeTab === 'hire' ? 'pixel-btn-green' : ''}`}
          >
            👥 HIRE STAFF (招聘)
          </button>
        </div>

        {/* Shop Contents */}
        <div className="flex-1 overflow-y-auto p-4 bg-[#a25927]/40 min-h-[300px]">
          
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
                    className="pixel-border-paper bg-parchment p-3 flex justify-between items-center gap-4"
                  >
                    <div className="flex-1">
                      <div className="flex justify-between items-baseline mb-1">
                        <h4 className="text-[20px] font-bold text-charcoal-brown leading-none">
                          {u.name}
                        </h4>
                        <span className="font-pixel text-[9px] text-grey-brown">
                          Level {u.level}/{u.maxLevel}
                        </span>
                      </div>
                      <p className="text-[16px] text-grey-brown leading-tight">
                        {u.description}
                      </p>
                    </div>

                    <div className="shrink-0 flex flex-col items-end gap-1.5">
                      {!isMax ? (
                        <>
                          <span className={`font-pixel text-[9px] font-bold ${canAfford ? 'text-crop-green' : 'text-berry-red'}`}>
                            💰 {cost}
                          </span>
                          <button 
                            onClick={() => handleUpgrade(u.id)}
                            disabled={!canAfford}
                            className={`pixel-btn py-1 px-3 text-[9px] ${!canAfford ? 'pixel-btn-disabled' : 'pixel-btn-green'}`}
                          >
                            UPGRADE
                          </button>
                        </>
                      ) : (
                        <span className="font-pixel text-[9px] text-grey-brown font-bold italic">
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
              <div className="pixel-border-paper bg-parchment p-4 flex-1 flex flex-col justify-between gap-4">
                <div>
                  <h4 className="text-[20px] font-bold text-charcoal-brown border-b border-parchment-dark pb-1.5 mb-3">
                    RECRUITMENT APPLICATION
                  </h4>

                  {/* Name field */}
                  <div className="mb-3">
                    <label className="text-[16px] text-grey-brown block mb-1">Employee Model Name:</label>
                    <input 
                      type="text"
                      value={hireName}
                      onChange={(e) => setHireName(e.target.value.slice(0, 16))}
                      className="w-full bg-parchment-dark border-2 border-charcoal-brown p-1.5 outline-none font-pixel text-[10px] text-charcoal-brown"
                    />
                  </div>

                  {/* Level Type field */}
                  <div className="mb-3">
                    <label className="text-[16px] text-grey-brown block mb-1">Role / Rank Tier:</label>
                    <div className="flex gap-2">
                      <button 
                        onClick={() => { sounds.playSelect(); setHireType('L3'); }}
                        className={`pixel-btn py-1.5 px-3 text-[8px] flex-1 ${hireType === 'L3' ? 'pixel-btn-green' : ''}`}
                      >
                        L3 Worker (Cost: 💰300)
                      </button>
                      <button 
                        onClick={() => { sounds.playSelect(); setHireType('L2'); }}
                        className={`pixel-btn py-1.5 px-3 text-[8px] flex-1 ${hireType === 'L2' ? 'pixel-btn-green' : ''}`}
                      >
                        L2 Team Leader (Cost: 💰600)
                      </button>
                    </div>
                  </div>

                  {/* Gender Selection */}
                  <div className="mb-2">
                    <label className="text-[16px] text-grey-brown block mb-1">Gender / Style variant:</label>
                    <div className="flex gap-2">
                      <button 
                        onClick={() => { sounds.playSelect(); setHireGender('female'); }}
                        className={`pixel-btn py-1 px-3 text-[8px] flex-1 ${hireGender === 'female' ? 'pixel-btn-green' : ''}`}
                      >
                        👩 Female Variant
                      </button>
                      <button 
                        onClick={() => { sounds.playSelect(); setHireGender('male'); }}
                        className={`pixel-btn py-1 px-3 text-[8px] flex-1 ${hireGender === 'male' ? 'pixel-btn-green' : ''}`}
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
                  className={`pixel-btn py-3 text-[10px] w-full ${
                    tokens < (hireType === 'L2' ? 600 : 300) || !hireName.trim() ? 'pixel-btn-disabled' : 'pixel-btn-green'
                  }`}
                >
                  ✓ SIGN WORK CONTRACT
                </button>
              </div>

              {/* Staff Roster panel */}
              <div className="pixel-border-paper bg-parchment p-3 w-full md:w-60 flex flex-col">
                <span className="font-pixel text-[9px] text-grey-brown border-b border-parchment-dark pb-1 mb-2">
                  OFFICE ROSTER ({agents.length}/7 desks)
                </span>
                <div className="flex-1 overflow-y-auto flex flex-col gap-1.5 pr-1 max-h-[220px] md:max-h-[300px]">
                  {agents.map(a => (
                    <div key={a.id} className="border border-wood-pine bg-[#eadecc] p-1.5 flex justify-between items-center text-[16px]">
                      <div>
                        <div className="font-bold text-charcoal-brown leading-none">{a.name}</div>
                        <div className="font-pixel text-[7px] text-grey-brown mt-0.5">{a.type} | {a.workstationId}</div>
                      </div>
                      <span className={`text-[12px] px-1 py-0.5 rounded ${
                        a.status === 'working' ? 'bg-[#7ca84c] text-white' :
                        a.status === 'error' ? 'bg-[#d83838] text-white' : 'bg-[#8c7662] text-white'
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
