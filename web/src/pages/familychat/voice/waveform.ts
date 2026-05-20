// computeWaveform decodes a recorded audio blob and downsamples the PCM into
// `barCount` RMS buckets normalized to [0, 1]. The result is used by the
// voice-note bubble to render a static waveform. The function is best-effort:
// any failure (unsupported platform, decode error) resolves to a flat array of
// zeros and a zero duration so the caller can persist a stable shape.
//
// The encoder hands us audio/webm;codecs=opus (or audio/ogg;codecs=opus on
// Firefox); both decode through the same OfflineAudioContext path. We only
// look at channel 0 — voice notes are effectively mono, and a per-channel
// mix would be overkill for the 32-bucket downsample.

export interface Waveform {
  bars: number[]
  durationMs: number
}

interface OfflineAudioContextCtor {
  new (channels: number, length: number, sampleRate: number): OfflineAudioContext
  prototype: OfflineAudioContext
}

interface AudioContextCtor {
  new (options?: AudioContextOptions): AudioContext
}

export const DEFAULT_BAR_COUNT = 32

function getOfflineAudioContextCtor(): OfflineAudioContextCtor | null {
  if (typeof window === 'undefined') return null
  const w = window as unknown as {
    OfflineAudioContext?: OfflineAudioContextCtor
    webkitOfflineAudioContext?: OfflineAudioContextCtor
  }
  return w.OfflineAudioContext ?? w.webkitOfflineAudioContext ?? null
}

function getAudioContextCtor(): AudioContextCtor | null {
  if (typeof window === 'undefined') return null
  const w = window as unknown as {
    AudioContext?: AudioContextCtor
    webkitAudioContext?: AudioContextCtor
  }
  return w.AudioContext ?? w.webkitAudioContext ?? null
}

function emptyWaveform(barCount: number): Waveform {
  return { bars: new Array(barCount).fill(0), durationMs: 0 }
}

// downsampleRMS splits the channel data into `barCount` equal-size windows and
// returns the RMS of each window. The final bar absorbs the remainder when
// length is not divisible by barCount; this keeps the bar count fixed.
function downsampleRMS(samples: Float32Array, barCount: number): number[] {
  const length = samples.length
  if (length === 0 || barCount <= 0) return new Array(Math.max(0, barCount)).fill(0)
  const bars = new Array<number>(barCount)
  const window = Math.floor(length / barCount)
  for (let i = 0; i < barCount; i++) {
    const start = i * window
    const end = i === barCount - 1 ? length : Math.min(length, start + window)
    let sumSq = 0
    let count = 0
    for (let s = start; s < end; s++) {
      const v = samples[s]
      sumSq += v * v
      count++
    }
    bars[i] = count > 0 ? Math.sqrt(sumSq / count) : 0
  }
  let max = 0
  for (const v of bars) if (v > max) max = v
  if (max <= 0) return bars.map(() => 0)
  return bars.map(v => v / max)
}

export async function computeWaveform(
  blob: Blob,
  barCount: number = DEFAULT_BAR_COUNT,
): Promise<Waveform> {
  const OfflineCtor = getOfflineAudioContextCtor()
  const AudioCtor = getAudioContextCtor()
  if (!OfflineCtor && !AudioCtor) return emptyWaveform(barCount)

  let arrayBuffer: ArrayBuffer
  try {
    arrayBuffer = await blob.arrayBuffer()
  } catch {
    return emptyWaveform(barCount)
  }

  // Prefer OfflineAudioContext.decodeAudioData when available — it does not
  // require user activation and works in background tabs. Fall back to a
  // real AudioContext for older Safari which only exposes decode on the
  // realtime context.
  let decoded: AudioBuffer | null = null
  if (OfflineCtor) {
    try {
      // The constructor needs concrete numbers; we throw the instance away
      // immediately after decode. Picking 1ch / 1 sample / 44100 keeps it
      // cheap. decodeAudioData internally honours the embedded sample rate.
      const probe = new OfflineCtor(1, 1, 44100)
      decoded = await probe.decodeAudioData(arrayBuffer.slice(0))
    } catch {
      decoded = null
    }
  }
  if (!decoded && AudioCtor) {
    let probe: AudioContext | null = null
    try {
      probe = new AudioCtor()
      decoded = await probe.decodeAudioData(arrayBuffer.slice(0))
    } catch {
      decoded = null
    } finally {
      try {
        void probe?.close?.().catch(() => {})
      } catch {
        // close() may throw synchronously on older Safari; ignore.
      }
    }
  }
  if (!decoded) return emptyWaveform(barCount)

  const channel = decoded.getChannelData(0)
  const bars = downsampleRMS(channel, barCount)
  const durationMs = Math.max(0, Math.round(decoded.duration * 1000))
  return { bars, durationMs }
}

// waveformLocalStorageKey is exported so the recorder, the bubble, and the
// chat view can agree on where to read/write a cached waveform without the
// meta_json round-trip. Keep it stable — old caches outlive deploys.
export function waveformLocalStorageKey(messageId: number | string): string {
  return `voice-waveform:${messageId}`
}

// readCachedWaveform returns the cached waveform for the given message id, or
// null if nothing is cached / the cache is corrupt. Safe to call in any
// environment — falls back to null when localStorage is unavailable.
export function readCachedWaveform(messageId: number | string): Waveform | null {
  if (typeof window === 'undefined') return null
  try {
    const raw = window.localStorage?.getItem(waveformLocalStorageKey(messageId))
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    if (
      parsed
      && typeof parsed === 'object'
      && Array.isArray((parsed as { bars?: unknown }).bars)
      && typeof (parsed as { durationMs?: unknown }).durationMs === 'number'
    ) {
      const rawBars = (parsed as { bars: unknown[] }).bars.slice(0, DEFAULT_BAR_COUNT).map(v =>
        typeof v === 'number' && Number.isFinite(v) ? Math.max(0, Math.min(1, v)) : 0,
      )
      const bars = rawBars.length < DEFAULT_BAR_COUNT
        ? [...rawBars, ...new Array(DEFAULT_BAR_COUNT - rawBars.length).fill(0)]
        : rawBars
      const rawDuration = (parsed as { durationMs: number }).durationMs
      const durationMs = Number.isFinite(rawDuration) && rawDuration >= 0 ? rawDuration : 0
      return { bars, durationMs }
    }
  } catch {
    // Ignore parse / storage errors.
  }
  return null
}

// writeCachedWaveform persists a waveform locally so the bubble can render
// instantly on reload when the backend doesn't yet attach meta_json. Errors
// are swallowed — caching is opportunistic.
export function writeCachedWaveform(messageId: number | string, waveform: Waveform): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage?.setItem(
      waveformLocalStorageKey(messageId),
      JSON.stringify(waveform),
    )
  } catch {
    // Quota exhausted or storage disabled — nothing to do.
  }
}
