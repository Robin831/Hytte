// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { SoundEngine } from './sound'

interface MockAudio {
  src: string
  preload: string
  currentTime: number
  load: ReturnType<typeof vi.fn>
  play: ReturnType<typeof vi.fn>
  canPlayType: ReturnType<typeof vi.fn>
}

function installAudioMock(canPlay: (type: string) => '' | 'maybe' | 'probably') {
  const instances: MockAudio[] = []
  const ctor = vi.fn().mockImplementation(() => {
    const a: MockAudio = {
      src: '',
      preload: '',
      currentTime: 0,
      load: vi.fn(),
      play: vi.fn(() => Promise.resolve()),
      canPlayType: vi.fn(canPlay),
    }
    instances.push(a)
    return a
  })
  vi.stubGlobal('Audio', ctor)
  // document.createElement('audio') is used by pickSource() to probe formats.
  const realCreate = document.createElement.bind(document)
  const createSpy = vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
    if (tag === 'audio') {
      const el = realCreate('audio')
      // Override canPlayType to match the mock policy.
      Object.defineProperty(el, 'canPlayType', { value: canPlay, configurable: true })
      return el
    }
    return realCreate(tag)
  })
  return { instances, ctor, createSpy }
}

describe('SoundEngine', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('preload creates one Audio per sound and loads it', () => {
    const { instances, ctor } = installAudioMock(() => 'probably')
    const engine = new SoundEngine()
    engine.preload()
    // correct, wrong, fanfare, milestone
    expect(ctor).toHaveBeenCalledTimes(4)
    expect(instances).toHaveLength(4)
    for (const a of instances) {
      expect(a.load).toHaveBeenCalled()
      expect(a.preload).toBe('auto')
    }
  })

  it('preload is idempotent', () => {
    const { ctor } = installAudioMock(() => 'probably')
    const engine = new SoundEngine()
    engine.preload()
    engine.preload()
    expect(ctor).toHaveBeenCalledTimes(4)
  })

  it('uses the wav source (universal browser support)', () => {
    const { instances } = installAudioMock(type => (type === 'audio/wav' ? 'maybe' : ''))
    const engine = new SoundEngine()
    engine.preload()
    expect(instances.every(a => a.src.endsWith('.wav'))).toBe(true)
  })

  it('skips sounds the browser cannot play', () => {
    const { instances, ctor } = installAudioMock(() => '')
    const engine = new SoundEngine()
    engine.preload()
    expect(ctor).not.toHaveBeenCalled()
    expect(instances).toHaveLength(0)
    // play() should be a safe no-op when no sources were registered.
    expect(() => engine.play('correct')).not.toThrow()
  })

  it('play resets currentTime and calls play()', () => {
    const { instances } = installAudioMock(() => 'probably')
    const engine = new SoundEngine()
    engine.preload()
    // Simulate that the audio has advanced.
    for (const a of instances) { a.currentTime = 4 }
    engine.play('correct')
    // The `correct` instance is the first registered.
    const correct = instances[0]
    expect(correct.currentTime).toBe(0)
    expect(correct.play).toHaveBeenCalled()
  })

  it('play is a no-op when muted', () => {
    const { instances } = installAudioMock(() => 'probably')
    const engine = new SoundEngine()
    engine.preload()
    engine.setMuted(true)
    engine.play('correct')
    for (const a of instances) {
      expect(a.play).not.toHaveBeenCalled()
    }
  })

  it('play lazy-preloads if not preloaded yet', () => {
    const { ctor } = installAudioMock(() => 'probably')
    const engine = new SoundEngine()
    engine.play('correct')
    expect(ctor).toHaveBeenCalledTimes(4)
  })

  it('play swallows autoplay rejection', async () => {
    const { instances } = installAudioMock(() => 'probably')
    const engine = new SoundEngine()
    engine.preload()
    for (const a of instances) {
      a.play = vi.fn(() => Promise.reject(new Error('NotAllowedError')))
    }
    // Should not throw synchronously; the rejection is caught internally.
    expect(() => engine.play('correct')).not.toThrow()
    // Let the microtask queue drain so any unhandled rejection would surface.
    await new Promise(r => setTimeout(r, 0))
  })

  it('isMuted reflects the latest setMuted call', () => {
    installAudioMock(() => 'probably')
    const engine = new SoundEngine()
    expect(engine.isMuted()).toBe(false)
    engine.setMuted(true)
    expect(engine.isMuted()).toBe(true)
    engine.setMuted(false)
    expect(engine.isMuted()).toBe(false)
  })
})
