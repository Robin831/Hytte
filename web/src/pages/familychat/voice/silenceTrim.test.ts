// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { trimLeadingTrailingSilence } from './silenceTrim'

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeFakeAudioBuffer(opts: {
  length?: number
  sampleRate?: number
  channels?: number
  /** Fill all samples with this value. */
  fillValue?: number
  /** Number of silent (0) samples at each end; middle is filled with 0.1. */
  silencePadding?: number
}): AudioBuffer {
  const { length = 44100, sampleRate = 44100, channels = 1, fillValue, silencePadding } = opts
  const datas: Float32Array[] = []
  for (let c = 0; c < channels; c++) {
    const data = new Float32Array(length)
    if (fillValue !== undefined) {
      data.fill(fillValue)
    } else if (silencePadding !== undefined) {
      data.fill(0.1, silencePadding, length - silencePadding)
    }
    datas.push(data)
  }
  return {
    sampleRate,
    numberOfChannels: channels,
    length,
    duration: length / sampleRate,
    getChannelData: (c: number) => datas[c],
    copyToChannel: (source: Float32Array, channelNumber: number) => {
      datas[channelNumber].set(source)
    },
    copyFromChannel: vi.fn(),
  } as unknown as AudioBuffer
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('trimLeadingTrailingSilence', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns original blob when AudioContext and OfflineAudioContext are unavailable', async () => {
    vi.stubGlobal('AudioContext', undefined)
    vi.stubGlobal('webkitAudioContext', undefined)
    vi.stubGlobal('OfflineAudioContext', undefined)
    vi.stubGlobal('webkitOfflineAudioContext', undefined)

    const blob = new Blob([new Uint8Array([1, 2, 3])], { type: 'audio/webm' })
    const result = await trimLeadingTrailingSilence(blob)
    expect(result).toBe(blob)
  })

  it('returns original blob when decodeAudioData rejects', async () => {
    class MockAudioContext {
      close = vi.fn().mockResolvedValue(undefined)
      decodeAudioData = vi.fn().mockRejectedValue(new Error('decode failed'))
    }
    class MockOfflineAudioContext {}

    vi.stubGlobal('AudioContext', MockAudioContext)
    vi.stubGlobal('OfflineAudioContext', MockOfflineAudioContext)

    const blob = new Blob([new Uint8Array([1, 2, 3])], { type: 'audio/webm' })
    const result = await trimLeadingTrailingSilence(blob)
    expect(result).toBe(blob)
  })

  it('returns original blob when the whole buffer is above the silence threshold', async () => {
    // All samples at 0.1 — start=0, end=length — no trimming needed.
    const audioBuffer = makeFakeAudioBuffer({ fillValue: 0.1 })

    class MockAudioContext {
      close = vi.fn().mockResolvedValue(undefined)
      decodeAudioData = vi.fn().mockResolvedValue(audioBuffer)
    }
    class MockOfflineAudioContext {}

    vi.stubGlobal('AudioContext', MockAudioContext)
    vi.stubGlobal('OfflineAudioContext', MockOfflineAudioContext)

    const blob = new Blob([new Uint8Array([1, 2, 3])], { type: 'audio/webm' })
    const result = await trimLeadingTrailingSilence(blob)
    expect(result).toBe(blob)
  })

  it('trims silence and returns a re-encoded blob with the original mime type', async () => {
    // Two window-widths of silence at each end (windowMs=20ms, sr=44100 → 882 samples/window).
    const SILENCE_PADDING = 882 * 2
    const audioBuffer = makeFakeAudioBuffer({ length: 44100, silencePadding: SILENCE_PADDING })
    const trimmedBuffer = makeFakeAudioBuffer({ length: 44100 - SILENCE_PADDING * 2, fillValue: 0.1 })

    const reEncodedContent = new Uint8Array([9, 8, 7])
    const mimeType = 'audio/webm'

    class MockMediaRecorder {
      ondataavailable: ((e: { data: Blob }) => void) | null = null
      onstop: (() => void) | null = null
      onerror: (() => void) | null = null
      state = 'inactive'
      mimeType: string

      constructor(_stream: unknown, options?: { mimeType?: string }) {
        this.mimeType = options?.mimeType ?? 'audio/webm'
      }

      start() {
        this.state = 'recording'
        Promise.resolve().then(() => {
          const blob = new Blob([reEncodedContent], { type: this.mimeType })
          this.ondataavailable?.({ data: blob })
          this.state = 'inactive'
          this.onstop?.()
        })
      }

      stop() { this.state = 'inactive' }

      static isTypeSupported = vi.fn().mockReturnValue(true)
    }

    class MockAudioContext {
      sampleRate = 44100
      close = vi.fn().mockResolvedValue(undefined)
      decodeAudioData = vi.fn().mockResolvedValue(audioBuffer)

      createMediaStreamDestination() {
        return { stream: {} }
      }

      createBufferSource() {
        const src: {
          buffer: unknown
          connect: ReturnType<typeof vi.fn>
          start: ReturnType<typeof vi.fn>
          onended: (() => void) | null
        } = {
          buffer: null,
          connect: vi.fn(),
          start: vi.fn().mockImplementation(() => {
            Promise.resolve().then(() => { src.onended?.() })
          }),
          onended: null,
        }
        return src
      }
    }

    class MockOfflineAudioContext {
      destination = {}

      constructor(
        public readonly numberOfChannels: number,
        public readonly length: number,
        public readonly sampleRate: number,
      ) {}

      createBuffer(channels: number, len: number, sr: number) {
        return makeFakeAudioBuffer({ channels, length: len, sampleRate: sr, fillValue: 0.1 })
      }

      createBufferSource() {
        return { buffer: null, connect: vi.fn(), start: vi.fn() }
      }

      startRendering() {
        return Promise.resolve(trimmedBuffer)
      }
    }

    vi.stubGlobal('AudioContext', MockAudioContext)
    vi.stubGlobal('OfflineAudioContext', MockOfflineAudioContext)
    vi.stubGlobal('MediaRecorder', MockMediaRecorder)

    const blob = new Blob([new Uint8Array([1, 2, 3])], { type: mimeType })
    const result = await trimLeadingTrailingSilence(blob)

    expect(result).not.toBe(blob)
    expect(result.type).toBe(mimeType)
    expect(result.size).toBeGreaterThan(0)
  })

  it('returns original blob when re-encoding fails (MediaRecorder unavailable)', async () => {
    const SILENCE_PADDING = 882 * 2
    const audioBuffer = makeFakeAudioBuffer({ length: 44100, silencePadding: SILENCE_PADDING })
    const trimmedBuffer = makeFakeAudioBuffer({ length: 44100 - SILENCE_PADDING * 2, fillValue: 0.1 })

    class MockAudioContext {
      sampleRate = 44100
      close = vi.fn().mockResolvedValue(undefined)
      decodeAudioData = vi.fn().mockResolvedValue(audioBuffer)
      createMediaStreamDestination() { return { stream: {} } }
      createBufferSource() {
        const src: { buffer: unknown; connect: ReturnType<typeof vi.fn>; start: ReturnType<typeof vi.fn>; onended: (() => void) | null } = {
          buffer: null,
          connect: vi.fn(),
          start: vi.fn().mockImplementation(() => { Promise.resolve().then(() => { src.onended?.() }) }),
          onended: null,
        }
        return src
      }
    }

    class MockOfflineAudioContext {
      destination = {}
      constructor(public numberOfChannels: number, public length: number, public sampleRate: number) {}
      createBuffer(c: number, l: number, sr: number) { return makeFakeAudioBuffer({ channels: c, length: l, sampleRate: sr, fillValue: 0.1 }) }
      createBufferSource() { return { buffer: null, connect: vi.fn(), start: vi.fn() } }
      startRendering() { return Promise.resolve(trimmedBuffer) }
    }

    vi.stubGlobal('AudioContext', MockAudioContext)
    vi.stubGlobal('OfflineAudioContext', MockOfflineAudioContext)
    vi.stubGlobal('MediaRecorder', undefined)

    const blob = new Blob([new Uint8Array([1, 2, 3])], { type: 'audio/webm' })
    const result = await trimLeadingTrailingSilence(blob)
    expect(result).toBe(blob)
  })
})
