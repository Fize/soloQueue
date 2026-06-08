import { useState, useEffect } from 'react'
import { useSimStore } from '../store/simStore'
import { sounds } from '../utils/audio'
import portraitMale from '../assets/portraits/portrait_secretary_male.png'
import portraitFemale from '../assets/portraits/portrait_secretary_female.png'

interface RegistrationSceneProps {
  onComplete: (modelRef?: string) => void
}

interface ModelOption {
  modelId: string
  modelName: string
  providerId: string
  apiModel: string
  contextWindow: number
}

interface ProviderOption {
  providerId: string
  providerName: string
}

export default function RegistrationScene({ onComplete }: RegistrationSceneProps) {
  const { registerL1 } = useSimStore()
  const [step, setStep] = useState(1)
  const [gender, setGender] = useState<'male' | 'female'>('female')
  const [name, setName] = useState('Alex')
  const [style, setStyle] = useState('friendly')

  // Model selection state
  const [modelStep, setModelStep] = useState<'loading' | 'found' | 'manual'>('loading')
  const [providers, setProviders] = useState<ProviderOption[]>([])
  const [models, setModels] = useState<ModelOption[]>([])
  const [selectedProvider, setSelectedProvider] = useState('')
  const [selectedModel, setSelectedModel] = useState('')

  // Manual config fields (when settings.toml not found)
  const [manualProvider, setManualProvider] = useState('deepseek')
  const [manualApiKey, setManualApiKey] = useState('')
  const [manualBaseUrl, setManualBaseUrl] = useState('')
  const [manualModel, setManualModel] = useState('deepseek-chat')

  // Load available models on mount
  useEffect(() => {
    if (typeof window.electronAPI?.getAvailableModels !== 'function') {
      // Running outside Electron — skip model step
      setModelStep('found')
      return
    }

    window.electronAPI.getAvailableModels().then((result) => {
      if (result.found && result.providers.length > 0 && result.models.length > 0) {
        setProviders(result.providers)
        setModels(result.models)
        setSelectedProvider(result.providers[0].providerId)
        const providerModels = result.models.filter(m => m.providerId === result.providers[0].providerId)
        if (providerModels.length > 0) {
          setSelectedModel(`${providerModels[0].providerId}:${providerModels[0].modelId}`)
        }
        setModelStep('found')
      } else {
        setModelStep('manual')
      }
    }).catch(() => {
      setModelStep('manual')
    })
  }, [])

  // Update selected models when provider changes
  const getModelsForProvider = (providerId: string) => {
    return models.filter(m => m.providerId === providerId)
  }

  const handleProviderChange = (pid: string) => {
    setSelectedProvider(pid)
    const pm = models.filter(m => m.providerId === pid)
    if (pm.length > 0) {
      setSelectedModel(`${pm[0].providerId}:${pm[0].modelId}`)
    } else {
      setSelectedModel('')
    }
  }

  const getSelectedModelRef = (): string => {
    if (modelStep === 'manual') {
      // Use the custom config model
      return manualModel
    }
    return selectedModel
  }

  const getModelDetails = (): string => {
    if (!selectedModel) return ''
    const [pid, mid] = selectedModel.split(':')
    const model = models.find(m => m.providerId === pid && m.modelId === mid)
    if (!model) return ''
    return `${model.apiModel} (${model.contextWindow > 0 ? `${Math.round(model.contextWindow / 1000)}K ctx` : 'context N/A'})`
  }

  const handleNext = () => {
    sounds.playSelect()
    setStep(s => s + 1)
  }

  const handleBack = () => {
    sounds.playSelect()
    setStep(s => Math.max(s - 1, 1))
  }

  const handleRegister = () => {
    sounds.playSelect()
    if (!name.trim()) return
    registerL1(name.trim(), gender, style)
    onComplete(getSelectedModelRef())
  }

  // Work styles
  const styles = [
    { id: 'friendly', name: 'Friendly & Supportive (友善热情)', desc: 'Friendly, encouraging, and detailed feedback.' },
    { id: 'professional', name: 'Professional & Direct (专业严谨)', desc: 'Formal, accurate, and concise documentation.' },
    { id: 'sarcastic', name: 'Witty & Sarcastic (幽默风趣)', desc: 'Lighthearted, joking, and clever prompts.' },
    { id: 'cold', name: 'Cold & Efficient (冷酷高效)', desc: 'Direct, emotionless, pure code logic output.' }
  ]

  const totalSteps = modelStep !== 'loading' ? 4 : 3

  return (
    <div className="relative w-screen h-screen flex justify-center items-center select-none font-retro bg-wood-pine p-6 pt-12">
      <div className="pixel-border-paper bg-parchment w-full max-w-2xl flex flex-col md:flex-row shadow-2xl overflow-hidden max-h-[520px]">
        {/* Left Side: Dynamic Character Portrait Card */}
        <div className="w-full md:w-2/5 bg-parchment-dark border-r-4 border-charcoal-brown p-4 flex flex-col items-center justify-center min-h-[200px]">
          <h3 className="font-pixel text-[10px] text-charcoal-brown mb-2 tracking-wide text-center">
            EMPLOYEE PROFILE
          </h3>
          <div className="pixel-border-wood p-1.5 bg-wood-oak w-36 h-36 flex items-center justify-center overflow-hidden">
            <img
              src={gender === 'male' ? portraitMale : portraitFemale}
              alt="Secretary Portrait"
              className="w-full h-full object-cover border-2 border-charcoal-brown image-rendering-pixelated"
            />
          </div>
          <span className="font-pixel text-[10px] text-grey-brown mt-4">
            ROLE: CHIEF SECRETARY
          </span>
          <span className="text-[18px] text-charcoal-brown font-bold tracking-wider mt-1">
            L1 AGENT
          </span>
        </div>

        {/* Right Side: Step Wizard Forms */}
        <div className="w-full md:w-3/5 p-6 flex flex-col justify-between min-h-[300px]">
          {/* Header Progress */}
          <div className="flex justify-between items-center mb-4 border-b-2 border-charcoal-brown pb-2">
            <span className="font-pixel text-[11px] text-charcoal-brown">NEW SECRETARY REGISTRY</span>
            <span className="font-pixel text-[10px] text-grey-brown">STEP {step}/{totalSteps}</span>
          </div>

          {/* Form Content */}
          <div className="flex-1 flex flex-col justify-center min-h-[220px]">
            {/* Step 1: Gender selection */}
            {step === 1 && (
              <div className="animate-in fade-in duration-200">
                <p className="text-[20px] text-charcoal-brown mb-4 font-bold">
                  Step 1: Choose Secretary Avatar
                </p>
                <div className="flex gap-4">
                  <button
                    onClick={() => { sounds.playSelect(); setGender('female'); }}
                    className={`pixel-btn flex-1 py-3 text-[10px] ${gender === 'female' ? 'pixel-btn-green' : ''}`}
                  >
                    👩 FEMALE (女性)
                  </button>
                  <button
                    onClick={() => { sounds.playSelect(); setGender('male'); }}
                    className={`pixel-btn flex-1 py-3 text-[10px] ${gender === 'male' ? 'pixel-btn-green' : ''}`}
                  >
                    👨 MALE (男性)
                  </button>
                </div>
                <p className="text-grey-brown text-[16px] mt-4 leading-tight">
                  This sets the visual sprite that will represent your L1 Secretary behind the reception desk.
                </p>
              </div>
            )}

            {/* Step 2: Name Input */}
            {step === 2 && (
              <div className="animate-in fade-in duration-200">
                <p className="text-[20px] text-charcoal-brown mb-2 font-bold">
                  Step 2: Assign Agent Name
                </p>
                <label className="text-[16px] text-grey-brown mb-2 block">
                  Please type in a name for your Chief Secretary:
                </label>
                <input
                  type="text"
                  value={name}
                  onChange={(e) => setName(e.target.value.slice(0, 16))}
                  className="w-full bg-parchment-dark border-4 border-charcoal-brown p-2 text-charcoal-brown outline-none font-pixel text-[12px] tracking-wide placeholder-grey-brown"
                  placeholder="Enter name..."
                />
                <p className="text-grey-brown text-[16px] mt-4 leading-tight">
                  Maximum 16 characters. This name is displayed on the workspace HUD and chat bubbles.
                </p>
              </div>
            )}

            {/* Step 3: Conversation Style Selection */}
            {step === 3 && (
              <div className="animate-in fade-in duration-200">
                <p className="text-[20px] text-charcoal-brown mb-2 font-bold">
                  Step 3: Define Communication Style
                </p>
                <div className="flex flex-col gap-2 max-h-[180px] overflow-y-auto pr-1">
                  {styles.map(s => (
                    <div
                      key={s.id}
                      onClick={() => { sounds.playSelect(); setStyle(s.id); }}
                      className={`border-2 p-2 cursor-pointer flex flex-col transition-colors ${
                        style === s.id
                          ? 'border-crop-green bg-[#edf6e8]'
                          : 'border-grey-brown hover:bg-[#eadecc]'
                      }`}
                    >
                      <span className="font-bold text-[18px] text-charcoal-brown leading-none">{s.name}</span>
                      <span className="text-[14px] text-grey-brown mt-0.5">{s.desc}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* Step 4: LLM Model Selection */}
            {step === 4 && modelStep === 'found' && (
              <div className="animate-in fade-in duration-200">
                <p className="text-[20px] text-charcoal-brown mb-2 font-bold">
                  Step 4: Choose L1 Intelligence Level
                </p>
                <p className="text-[14px] text-grey-brown mb-3">
                  Select the LLM model that powers your L1 secretary:
                </p>

                {/* Provider dropdown */}
                <label className="text-[16px] text-charcoal-brown font-bold block mb-1">
                  Provider / 供应商
                </label>
                <select
                  value={selectedProvider}
                  onChange={e => handleProviderChange(e.target.value)}
                  className="w-full bg-parchment-dark border-4 border-charcoal-brown p-2 text-charcoal-brown outline-none font-retro text-[14px] mb-3"
                >
                  {providers.map(p => (
                    <option key={p.providerId} value={p.providerId}>
                      {p.providerName || p.providerId}
                    </option>
                  ))}
                </select>

                {/* Model dropdown */}
                <label className="text-[16px] text-charcoal-brown font-bold block mb-1">
                  Model / 模型
                </label>
                <select
                  value={selectedModel}
                  onChange={e => setSelectedModel(e.target.value)}
                  className="w-full bg-parchment-dark border-4 border-charcoal-brown p-2 text-charcoal-brown outline-none font-retro text-[14px]"
                >
                  {getModelsForProvider(selectedProvider).map(m => (
                    <option key={m.modelId} value={`${m.providerId}:${m.modelId}`}>
                      {m.modelName} ({Math.round(m.contextWindow / 1000)}K ctx)
                    </option>
                  ))}
                </select>

                {/* Model info */}
                <div className="mt-3 p-2 bg-wood-oak/20 border-2 border-charcoal-brown">
                  <span className="text-[12px] text-charcoal-brown font-mono">
                    {getModelDetails()}
                  </span>
                </div>

                <p className="text-grey-brown text-[14px] mt-3 italic">
                  This selects the default model for L1 (universal) tasks. Can be changed later in settings.
                </p>
              </div>
            )}

            {/* Step 4: Manual config (no settings.toml found) */}
            {step === 4 && modelStep === 'manual' && (
              <div className="animate-in fade-in duration-200">
                <p className="text-[20px] text-charcoal-brown mb-2 font-bold">
                  Step 4: Configure LLM Provider
                </p>
                <p className="text-[14px] text-grey-brown mb-3">
                  No settings.toml found. Enter your provider info:
                </p>

                <label className="text-[14px] text-charcoal-brown font-bold block mb-1">
                  Provider Name / 供应商名称
                </label>
                <input
                  type="text"
                  value={manualProvider}
                  onChange={e => setManualProvider(e.target.value.slice(0, 32))}
                  className="w-full bg-parchment-dark border-4 border-charcoal-brown p-2 text-charcoal-brown outline-none font-retro text-[14px] mb-2"
                />

                <label className="text-[14px] text-charcoal-brown font-bold block mb-1">
                  API Key / 密钥
                </label>
                <input
                  type="password"
                  value={manualApiKey}
                  onChange={e => setManualApiKey(e.target.value)}
                  className="w-full bg-parchment-dark border-4 border-charcoal-brown p-2 text-charcoal-brown outline-none font-retro text-[14px] mb-2"
                />

                <label className="text-[14px] text-charcoal-brown font-bold block mb-1">
                  Base URL
                </label>
                <input
                  type="text"
                  value={manualBaseUrl}
                  onChange={e => setManualBaseUrl(e.target.value)}
                  className="w-full bg-parchment-dark border-4 border-charcoal-brown p-2 text-charcoal-brown outline-none font-retro text-[14px] mb-2"
                />

                <label className="text-[14px] text-charcoal-brown font-bold block mb-1">
                  Model Name
                </label>
                <input
                  type="text"
                  value={manualModel}
                  onChange={e => setManualModel(e.target.value)}
                  className="w-full bg-parchment-dark border-4 border-charcoal-brown p-2 text-charcoal-brown outline-none font-retro text-[14px]"
                />
              </div>
            )}

            {/* Loading state */}
            {step === 4 && modelStep === 'loading' && (
              <div className="animate-in fade-in duration-200 flex flex-col items-center justify-center py-8">
                <div className="w-8 h-8 border-4 border-charcoal-brown border-t-crop-green rounded-full animate-spin mb-4" />
                <p className="text-[16px] text-grey-brown">Loading available models...</p>
              </div>
            )}
          </div>

          {/* Navigation Controls */}
          <div className="flex justify-between mt-4 pt-2 border-t-2 border-charcoal-brown">
            {step > 1 ? (
              <button
                onClick={handleBack}
                className="pixel-btn pixel-btn-orange py-2 px-4 text-[10px]"
              >
                ← BACK
              </button>
            ) : (
              <div />
            )}

            {step < totalSteps ? (
              <button
                onClick={handleNext}
                disabled={step === 2 && !name.trim()}
                className={`pixel-btn pixel-btn-green py-2 px-4 text-[10px] ${
                  step === 2 && !name.trim() ? 'pixel-btn-disabled' : ''
                }`}
              >
                NEXT →
              </button>
            ) : (
              <button
                onClick={handleRegister}
                disabled={!name.trim()}
                className={`pixel-btn pixel-btn-green py-2 px-4 text-[10px] ${
                  !name.trim() ? 'pixel-btn-disabled' : ''
                }`}
              >
                ✓ HIRE & ONBOARD
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
