import { useState } from 'react'
import { useSimStore } from '../stores/simStore'
import { sounds } from '../utils/audio'
import portraitMale from '../assets/portraits/portrait_secretary_male.png'
import portraitFemale from '../assets/portraits/portrait_secretary_female.png'

interface RegistrationSceneProps {
  onComplete: () => void
}

export default function RegistrationScene({ onComplete }: RegistrationSceneProps) {
  const { registerL1 } = useSimStore()
  const [step, setStep] = useState(1)
  const [gender, setGender] = useState<'male' | 'female'>('female')
  const [name, setName] = useState('Alex')
  const [style, setStyle] = useState('friendly')

  const handleNext = () => {
    setStep(s => s + 1)
    try { sounds.playSelect() } catch {}
  }

  const handleBack = () => {
    setStep(s => Math.max(s - 1, 1))
    try { sounds.playSelect() } catch {}
  }

  const handleRegister = () => {
    if (!name.trim()) return
    registerL1(name.trim(), gender, style)
    try { sounds.playSelect() } catch {}
    onComplete()
  }

  // Work styles
  const styles = [
    { id: 'friendly', name: 'Friendly & Supportive (友善热情)', desc: 'Friendly, encouraging, and detailed feedback.' },
    { id: 'professional', name: 'Professional & Direct (专业严谨)', desc: 'Formal, accurate, and concise documentation.' },
    { id: 'sarcastic', name: 'Witty & Sarcastic (幽默风趣)', desc: 'Lighthearted, joking, and clever prompts.' },
    { id: 'cold', name: 'Cold & Efficient (冷酷高效)', desc: 'Direct, emotionless, pure code logic output.' }
  ]

  const totalSteps = 3

  return (
    <div className="fixed inset-0 z-50 bg-black/50 backdrop-blur-sm flex items-center justify-center p-4 select-none font-retro animate-in fade-in duration-200">
      <div className="bg-white border border-gray-200 w-full max-w-2xl flex flex-col md:flex-row shadow-2xl overflow-hidden rounded-xl max-h-[560px]">
        {/* Left Side: Dynamic Character Portrait Card */}
        <div className="w-full md:w-2/5 bg-gray-50 border-r border-gray-200 p-4 flex flex-col items-center justify-center min-h-[200px]">
          <h3 className="font-pixel text-[10px] text-primary mb-2 tracking-wide text-center">
            EMPLOYEE PROFILE
          </h3>
          <div className="p-1.5 bg-white border border-gray-200 rounded-lg w-36 h-36 flex items-center justify-center overflow-hidden">
            <img
              src={gender === 'male' ? portraitMale : portraitFemale}
              alt="Secretary Portrait"
              className="w-full h-full object-contain border border-gray-100 image-rendering-pixelated rounded"
            />
          </div>
          <span className="font-pixel text-[10px] text-gray-450 mt-4">
            ROLE: CHIEF SECRETARY
          </span>
          <span className="text-[18px] text-primary font-bold tracking-wider mt-1">
            L1 AGENT
          </span>
        </div>

        {/* Right Side: Step Wizard Forms */}
        <div className="w-full md:w-3/5 p-6 flex flex-col justify-between min-h-[300px] bg-white text-gray-800">
          {/* Header Progress */}
          <div className="flex justify-between items-center mb-4 border-b border-gray-200 pb-2">
            <span className="font-pixel text-[10px] text-primary tracking-wide">NEW SECRETARY REGISTRY</span>
            <span className="font-pixel text-[9px] text-gray-400 font-bold">STEP {step}/{totalSteps}</span>
          </div>

          {/* Form Content */}
          <div className="flex-1 flex flex-col justify-center min-h-[220px]">
            {/* Step 1: Gender selection */}
            {step === 1 && (
              <div className="animate-in fade-in duration-200">
                <p className="text-[18px] text-gray-900 mb-4 font-bold">
                  Step 1: Choose Secretary Avatar
                </p>
                <div className="flex gap-4">
                  <button
                    onClick={() => { setGender('female'); try { sounds.playSelect() } catch {} }}
                    className={`flex-1 py-3 text-[11px] font-bold rounded border transition-all ${
                      gender === 'female'
                        ? 'bg-primary/10 text-primary border-primary'
                        : 'border-gray-200 text-gray-450 hover:text-gray-700 hover:bg-gray-50'
                    }`}
                  >
                    👩 FEMALE (女性)
                  </button>
                  <button
                    onClick={() => { setGender('male'); try { sounds.playSelect() } catch {} }}
                    className={`flex-1 py-3 text-[11px] font-bold rounded border transition-all ${
                      gender === 'male'
                        ? 'bg-primary/10 text-primary border-primary'
                        : 'border-gray-200 text-gray-450 hover:text-gray-700 hover:bg-gray-50'
                    }`}
                  >
                    👨 MALE (男性)
                  </button>
                </div>
                <p className="text-gray-450 text-[12px] mt-4 leading-normal">
                  This sets the visual sprite that will represent your L1 Secretary behind the reception desk.
                </p>
              </div>
            )}

            {/* Step 2: Name Input */}
            {step === 2 && (
              <div className="animate-in fade-in duration-200">
                <p className="text-[18px] text-gray-900 mb-2 font-bold">
                  Step 2: Assign Agent Name
                </p>
                <label className="text-[12px] text-gray-450 mb-2 block">
                  Please type in a name for your Chief Secretary:
                </label>
                <input
                  type="text"
                  value={name}
                  onChange={(e) => setName(e.target.value.slice(0, 16))}
                  className="w-full bg-white border border-gray-300 rounded p-2 text-gray-800 outline-none font-pixel text-[10px] tracking-wide placeholder-gray-400 focus:border-primary transition-colors"
                  placeholder="Enter name..."
                />
                <p className="text-gray-450 text-[12px] mt-4 leading-normal">
                  Maximum 16 characters. This name is displayed on the workspace HUD and chat bubbles.
                </p>
              </div>
            )}

            {/* Step 3: Conversation Style Selection */}
            {step === 3 && (
              <div className="animate-in fade-in duration-200">
                <p className="text-[18px] text-gray-900 mb-2 font-bold">
                  Step 3: Define Communication Style
                </p>
                <div className="flex flex-col gap-2 max-h-[180px] overflow-y-auto pr-1">
                  {styles.map(s => (
                    <div
                      key={s.id}
                      onClick={() => { setStyle(s.id); try { sounds.playSelect() } catch {} }}
                      className={`border rounded p-2 cursor-pointer flex flex-col transition-all ${
                        style === s.id
                          ? 'border-primary bg-primary/10 text-gray-900 font-bold'
                          : 'border-gray-200 hover:bg-gray-50 text-gray-450'
                      }`}
                    >
                      <span className="font-bold text-[14px] leading-tight">{s.name}</span>
                      <span className="text-[11px] mt-0.5 opacity-80">{s.desc}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>

          {/* Navigation Controls */}
          <div className="flex justify-between mt-4 pt-2 border-t border-gray-200">
            {step > 1 ? (
              <button
                onClick={handleBack}
                className="py-1.5 px-4 text-[11px] rounded border border-gray-300 text-gray-600 hover:bg-gray-50 transition-colors"
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
                className={`py-1.5 px-4 text-[11px] rounded font-bold transition-colors ${
                  step === 2 && !name.trim()
                    ? 'bg-gray-100 border border-gray-200 text-gray-400 cursor-not-allowed'
                    : 'bg-primary text-primary-foreground hover:bg-primary/95'
                }`}
              >
                NEXT →
              </button>
            ) : (
              <button
                onClick={handleRegister}
                disabled={!name.trim()}
                className={`py-1.5 px-4 text-[11px] rounded font-bold transition-colors ${
                  !name.trim()
                    ? 'bg-gray-100 border border-gray-200 text-gray-400 cursor-not-allowed'
                    : 'bg-primary text-primary-foreground hover:bg-primary/95'
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
