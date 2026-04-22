export type SoundName = 'correct' | 'wrong' | 'fanfare' | 'milestone'

interface SoundSource {
  src: string
  type: string
}

// Every sound maps to an ordered list of candidate encodings — the first
// the browser claims to support wins. Only formats we actually ship should
// appear here; otherwise canPlayType will pick a format we don't have on
// disk and each play() will 404. WAV is safe across all modern browsers
// (Safari, iOS, Chrome, Firefox) so it's currently the only format we ship.
// When .ogg / .mp3 assets land, add them above the WAV entry so better
// formats are preferred where available.
const SOURCES: Record<SoundName, SoundSource[]> = {
  correct: [{ src: '/sounds/regnemester/correct.wav', type: 'audio/wav' }],
  wrong: [{ src: '/sounds/regnemester/wrong.wav', type: 'audio/wav' }],
  fanfare: [{ src: '/sounds/regnemester/fanfare.wav', type: 'audio/wav' }],
  milestone: [{ src: '/sounds/regnemester/milestone.wav', type: 'audio/wav' }],
}

const SOUND_NAMES: readonly SoundName[] = ['correct', 'wrong', 'fanfare', 'milestone']

function canPlay(audio: HTMLAudioElement, type: string): boolean {
  const result = audio.canPlayType(type)
  return result === 'probably' || result === 'maybe'
}

function pickSource(sources: SoundSource[]): SoundSource | null {
  if (typeof document === 'undefined') return null
  const probe = document.createElement('audio')
  for (const s of sources) {
    if (canPlay(probe, s.type)) return s
  }
  return null
}

class SoundEngine {
  private buffers: Partial<Record<SoundName, HTMLAudioElement>> = {}
  private muted = false
  private preloaded = false

  preload(): void {
    if (this.preloaded) return
    if (typeof Audio === 'undefined') return
    for (const name of SOUND_NAMES) {
      const src = pickSource(SOURCES[name])
      if (!src) continue
      const a = new Audio()
      a.preload = 'auto'
      a.src = src.src
      // Most browsers defer actual bytes until load() is called; calling it
      // here warms the cache so the first play() is instant.
      try { a.load() } catch { /* no-op — browsers may throw on test doubles */ }
      this.buffers[name] = a
    }
    this.preloaded = true
  }

  setMuted(muted: boolean): void {
    this.muted = muted
  }

  isMuted(): boolean {
    return this.muted
  }

  play(name: SoundName): void {
    if (this.muted) return
    // Lazy-preload so the first play() still works if the caller forgot
    // to call preload() (e.g. during tests or rapid mount/unmount cycles).
    if (!this.preloaded) this.preload()
    const a = this.buffers[name]
    if (!a) return
    try {
      a.currentTime = 0
    } catch {
      // Some browsers throw if the media isn't ready yet — the play() call
      // below will still fire from the start, which is good enough.
    }
    const p = a.play()
    // Autoplay policies reject play() promises when the user hasn't interacted
    // with the page yet; swallow silently so we don't crash the game loop.
    if (p && typeof p.catch === 'function') {
      p.catch(() => { /* no-op */ })
    }
  }

  // Exposed for tests — releases loaded elements so GC can reclaim them.
  reset(): void {
    this.buffers = {}
    this.preloaded = false
  }
}

export const soundEngine = new SoundEngine()
export { SoundEngine }
