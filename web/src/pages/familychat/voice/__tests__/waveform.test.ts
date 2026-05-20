// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import {
  computeWaveform,
  readCachedWaveform,
  writeCachedWaveform,
  waveformLocalStorageKey,
  DEFAULT_BAR_COUNT,
} from '../waveform'

function makeBlob(): Blob {
  return new Blob([new Uint8Array([1, 2, 3, 4])], { type: 'audio/webm' })
}

function makeDecodedBuffer(opts: { length: number; sampleRate: number; sample: (i: number) => number }): AudioBuffer {
  const data = new Float32Array(opts.length)
  for (let i = 0; i < opts.length; i++) data[i] = opts.sample(i)
  return {
    sampleRate: opts.sampleRate,
    numberOfChannels: 1,
    length: opts.length,
    duration: opts.length / opts.sampleRate,
    getChannelData: () => data,
    copyToChannel: () => undefined,
    copyFromChannel: () => undefined,
  } as unknown as AudioBuffer
}

afterEach(() => {
  vi.unstubAllGlobals()
  try { window.localStorage.clear() } catch { /* ignore */ }
})

describe('computeWaveform', () => {
  it('returns 32 zero bars + zero duration when no AudioContext is available', async () => {
    vi.stubGlobal('AudioContext', undefined)
    vi.stubGlobal('webkitAudioContext', undefined)
    vi.stubGlobal('OfflineAudioContext', undefined)
    vi.stubGlobal('webkitOfflineAudioContext', undefined)

    const result = await computeWaveform(makeBlob())
    expect(result.bars).toHaveLength(DEFAULT_BAR_COUNT)
    expect(result.bars.every(v => v === 0)).toBe(true)
    expect(result.durationMs).toBe(0)
  })

  it('downsamples PCM into 32 normalized RMS buckets', async () => {
    const sampleRate = 16000
    const length = sampleRate * 2 // 2 seconds
    const buffer = makeDecodedBuffer({
      length,
      sampleRate,
      // Ramp from 0 to 1 across the buffer so each bucket gets progressively
      // louder. The normalized bars must therefore be strictly non-decreasing.
      sample: i => i / length,
    })

    class MockOfflineCtx {
      decodeAudioData = vi.fn().mockResolvedValue(buffer)
    }
    class MockAudioCtx {
      close = vi.fn().mockResolvedValue(undefined)
      decodeAudioData = vi.fn().mockResolvedValue(buffer)
    }
    vi.stubGlobal('OfflineAudioContext', MockOfflineCtx)
    vi.stubGlobal('AudioContext', MockAudioCtx)

    const result = await computeWaveform(makeBlob())
    expect(result.bars).toHaveLength(DEFAULT_BAR_COUNT)
    expect(result.durationMs).toBe(2000)

    // Normalized to [0,1] with at least one bar at the max.
    const max = Math.max(...result.bars)
    expect(max).toBeCloseTo(1, 5)
    for (const v of result.bars) {
      expect(v).toBeGreaterThanOrEqual(0)
      expect(v).toBeLessThanOrEqual(1)
    }

    // Buckets get louder as we move right, so the last bar dominates the first.
    expect(result.bars[result.bars.length - 1]).toBeGreaterThan(result.bars[0])
  })

  it('returns the empty fallback when decodeAudioData rejects in both paths', async () => {
    class MockOfflineCtx {
      decodeAudioData = vi.fn().mockRejectedValue(new Error('boom'))
    }
    class MockAudioCtx {
      close = vi.fn().mockResolvedValue(undefined)
      decodeAudioData = vi.fn().mockRejectedValue(new Error('boom'))
    }
    vi.stubGlobal('OfflineAudioContext', MockOfflineCtx)
    vi.stubGlobal('AudioContext', MockAudioCtx)

    const result = await computeWaveform(makeBlob())
    expect(result.bars).toHaveLength(DEFAULT_BAR_COUNT)
    expect(result.bars.every(v => v === 0)).toBe(true)
    expect(result.durationMs).toBe(0)
  })
})

describe('waveform localStorage cache', () => {
  it('round-trips a waveform under the expected key', () => {
    const waveform = { bars: [0.1, 0.5, 1, 0], durationMs: 1234 }
    writeCachedWaveform(99, waveform)
    expect(window.localStorage.getItem(waveformLocalStorageKey(99))).toContain('"durationMs":1234')

    const read = readCachedWaveform(99)
    expect(read).not.toBeNull()
    expect(read!.durationMs).toBe(1234)
    expect(read!.bars).toHaveLength(DEFAULT_BAR_COUNT)
    expect(read!.bars.slice(0, 4)).toEqual([0.1, 0.5, 1, 0])
    expect(read!.bars.slice(4).every(v => v === 0)).toBe(true)
  })

  it('returns null for missing or malformed cache entries', () => {
    expect(readCachedWaveform('missing')).toBeNull()
    window.localStorage.setItem(waveformLocalStorageKey('bad'), '{not json')
    expect(readCachedWaveform('bad')).toBeNull()
    window.localStorage.setItem(waveformLocalStorageKey('shape'), JSON.stringify({ foo: 1 }))
    expect(readCachedWaveform('shape')).toBeNull()
  })

  it('clamps bar values into [0,1] when reading', () => {
    window.localStorage.setItem(
      waveformLocalStorageKey('weird'),
      JSON.stringify({ bars: [-1, 2, 'x', 0.3], durationMs: 100 }),
    )
    const read = readCachedWaveform('weird')
    expect(read).not.toBeNull()
    expect(read!.bars).toHaveLength(DEFAULT_BAR_COUNT)
    expect(read!.bars.slice(0, 4)).toEqual([0, 1, 0, 0.3])
    expect(read!.bars.slice(4).every(v => v === 0)).toBe(true)
  })
})
