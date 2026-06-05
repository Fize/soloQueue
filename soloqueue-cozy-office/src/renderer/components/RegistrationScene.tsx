import { useState } from 'react'
import { useSimStore } from '../store/simStore'
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
    onComplete()
  }

  // Work styles
  const styles = [
    { id: 'friendly', name: 'Friendly & Supportive (友善热情)', desc: 'Friendly, encouraging, and detailed feedback.' },
    { id: 'professional', name: 'Professional & Direct (专业严谨)', desc: 'Formal, accurate, and concise documentation.' },
    { id: 'sarcastic', name: 'Witty & Sarcastic (幽默风趣)', desc: 'Lighthearted, joking, and clever prompts.' },
    { id: 'cold', name: 'Cold & Efficient (冷酷高效)', desc: 'Direct, emotionless, pure code logic output.' }
  ]

  return (
    <div className="relative w-screen h-screen flex justify-center items-center select-none font-retro bg-wood-pine p-6 pt-12">
      <div className="pixel-border-paper bg-parchment w-full max-w-2xl flex flex-col md:flex-row shadow-2xl overflow-hidden max-h-[500px]">
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
            <span className="font-pixel text-[10px] text-grey-brown">STEP {step}/3</span>
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

            {step < 3 ? (
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
