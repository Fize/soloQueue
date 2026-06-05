class SoundEffectsManager {
  private ctx: AudioContext | null = null

  private init() {
    if (!this.ctx) {
      // Lazy load to avoid browser autoplacing blocks before interaction
      const AudioContextClass = window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext
      if (AudioContextClass) {
        this.ctx = new AudioContextClass()
      }
    }
  }

  playSelect() {
    this.init()
    if (!this.ctx) return

    const osc = this.ctx.createOscillator()
    const gain = this.ctx.createGain()

    osc.type = 'square' // Retro 8-bit sound
    osc.frequency.setValueAtTime(600, this.ctx.currentTime)
    osc.frequency.exponentialRampToValueAtTime(1000, this.ctx.currentTime + 0.1)

    gain.gain.setValueAtTime(0.08, this.ctx.currentTime)
    gain.gain.exponentialRampToValueAtTime(0.001, this.ctx.currentTime + 0.1)

    osc.connect(gain)
    gain.connect(this.ctx.destination)

    osc.start()
    osc.stop(this.ctx.currentTime + 0.1)
  }

  playSuccess() {
    this.init()
    if (!this.ctx) return

    const now = this.ctx.currentTime

    // Retro level up / success: two quick notes
    const playNote = (freq: number, start: number, duration: number) => {
      if (!this.ctx) return
      const osc = this.ctx.createOscillator()
      const gain = this.ctx.createGain()

      osc.type = 'triangle'
      osc.frequency.setValueAtTime(freq, start)

      gain.gain.setValueAtTime(0.1, start)
      gain.gain.exponentialRampToValueAtTime(0.001, start + duration)

      osc.connect(gain)
      gain.connect(this.ctx.destination)

      osc.start(start)
      osc.stop(start + duration)
    }

    playNote(523.25, now, 0.08) // C5
    playNote(659.25, now + 0.08, 0.08) // E5
    playNote(783.99, now + 0.16, 0.16) // G5
  }

  playError() {
    this.init()
    if (!this.ctx) return

    const osc = this.ctx.createOscillator()
    const gain = this.ctx.createGain()

    osc.type = 'sawtooth' // Gritty buzz
    osc.frequency.setValueAtTime(180, this.ctx.currentTime)
    osc.frequency.linearRampToValueAtTime(80, this.ctx.currentTime + 0.3)

    gain.gain.setValueAtTime(0.12, this.ctx.currentTime)
    gain.gain.linearRampToValueAtTime(0.001, this.ctx.currentTime + 0.3)

    osc.connect(gain)
    gain.connect(this.ctx.destination)

    osc.start()
    osc.stop(this.ctx.currentTime + 0.3)
  }

  playUpgrade() {
    this.init()
    if (!this.ctx) return

    const now = this.ctx.currentTime
    const notes = [261.63, 329.63, 392.00, 523.25] // C4, E4, G4, C5 arpeggio

    notes.forEach((freq, index) => {
      if (!this.ctx) return
      const osc = this.ctx.createOscillator()
      const gain = this.ctx.createGain()

      osc.type = 'sine'
      osc.frequency.setValueAtTime(freq, now + index * 0.1)

      gain.gain.setValueAtTime(0.1, now + index * 0.1)
      gain.gain.exponentialRampToValueAtTime(0.001, now + index * 0.1 + 0.2)

      osc.connect(gain)
      gain.connect(this.ctx.destination)

      osc.start(now + index * 0.1)
      osc.stop(now + index * 0.1 + 0.2)
    })
  }
}

export const sounds = new SoundEffectsManager()
