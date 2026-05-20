// trimLeadingTrailingSilence removes leading and trailing silence from a
// MediaRecorder blob. The trim is best-effort: any failure (unsupported
// platform, decode error, missing re-encoder) returns the original blob, so
// the caller can safely chain this in front of an upload step.
//
// The "threshold on Float32 PCM via OfflineAudioContext" approach decodes the
// recorded blob, scans the channel data in fixed-size windows for the first
// and last sample above a silence threshold, then re-renders the trimmed
// PCM through an OfflineAudioContext and re-encodes it via a real
// AudioContext + MediaStreamDestination + MediaRecorder so the upload
// remains audio/webm (or whatever the recorder emits).

export interface TrimOptions {
  // Absolute amplitude threshold in [0, 1]. Samples below are silence.
  thresholdAmplitude?: number
  // Don't trim if the result would be shorter than this. Avoids cutting off
  // very short voice notes ("hi!") down to nothing.
  minDurationMs?: number
  // Window size for silence detection. Smaller is more precise but slower.
  windowMs?: number
  // Outer cap on the re-encode step. If we can't re-encode in this many ms,
  // give up and return the original blob.
  reencodeTimeoutMs?: number
}

const DEFAULTS = {
  thresholdAmplitude: 0.015,
  minDurationMs: 350,
  windowMs: 20,
  reencodeTimeoutMs: 8000,
} satisfies Required<TrimOptions>

interface OfflineAudioContextCtor {
  new (channels: number, length: number, sampleRate: number): OfflineAudioContext
}

interface AudioContextCtor {
  new (options?: AudioContextOptions): AudioContext
}

function getOfflineAudioContextCtor(): OfflineAudioContextCtor | null {
  if (typeof window === 'undefined') return null
  const w = window as unknown as { OfflineAudioContext?: OfflineAudioContextCtor; webkitOfflineAudioContext?: OfflineAudioContextCtor }
  return w.OfflineAudioContext ?? w.webkitOfflineAudioContext ?? null
}

function getAudioContextCtor(): AudioContextCtor | null {
  if (typeof window === 'undefined') return null
  const w = window as unknown as { AudioContext?: AudioContextCtor; webkitAudioContext?: AudioContextCtor }
  return w.AudioContext ?? w.webkitAudioContext ?? null
}

function findSilenceBoundaries(
  buffer: AudioBuffer,
  thresholdAmplitude: number,
  windowSamples: number,
): { start: number; end: number } {
  const length = buffer.length
  const channels = buffer.numberOfChannels
  const channelData: Float32Array[] = []
  for (let c = 0; c < channels; c++) {
    channelData.push(buffer.getChannelData(c))
  }
  const maxAbsInWindow = (winStart: number): number => {
    const winEnd = Math.min(length, winStart + windowSamples)
    let m = 0
    for (let c = 0; c < channels; c++) {
      const data = channelData[c]
      for (let i = winStart; i < winEnd; i++) {
        const v = Math.abs(data[i])
        if (v > m) m = v
      }
    }
    return m
  }
  let start = 0
  for (let i = 0; i < length; i += windowSamples) {
    if (maxAbsInWindow(i) >= thresholdAmplitude) {
      start = i
      break
    }
  }
  let end = length
  for (let i = length - windowSamples; i >= 0; i -= windowSamples) {
    if (maxAbsInWindow(i) >= thresholdAmplitude) {
      end = Math.min(length, i + windowSamples)
      break
    }
  }
  if (end <= start) {
    // Whole buffer is below threshold — leave it alone so we don't blank the
    // recording when a microphone undershoots our threshold.
    return { start: 0, end: length }
  }
  return { start, end }
}

async function reencodeTrimmedBuffer(
  rendered: AudioBuffer,
  mimeType: string,
  timeoutMs: number,
): Promise<Blob | null> {
  if (typeof MediaRecorder === 'undefined') return null
  const AudioCtor = getAudioContextCtor()
  if (!AudioCtor) return null

  const ctx = new AudioCtor({ sampleRate: rendered.sampleRate })
  const dest = ctx.createMediaStreamDestination()
  const src = ctx.createBufferSource()
  src.buffer = rendered
  src.connect(dest)

  let recorder: MediaRecorder
  try {
    recorder = mimeType && MediaRecorder.isTypeSupported?.(mimeType)
      ? new MediaRecorder(dest.stream, { mimeType })
      : new MediaRecorder(dest.stream)
  } catch {
    void ctx.close?.().catch(() => {})
    return null
  }

  const chunks: BlobPart[] = []
  recorder.ondataavailable = (event: BlobEvent) => {
    if (event.data && event.data.size > 0) chunks.push(event.data)
  }

  return new Promise<Blob | null>(resolve => {
    let settled = false
    const finish = (value: Blob | null) => {
      if (settled) return
      settled = true
      void ctx.close?.().catch(() => {})
      resolve(value)
    }
    recorder.onerror = () => finish(null)
    recorder.onstop = () => {
      if (chunks.length === 0) {
        finish(null)
        return
      }
      finish(new Blob(chunks, { type: recorder.mimeType || mimeType }))
    }

    src.onended = () => {
      // src finishes when the buffer is fully scheduled. Recorder still needs
      // a tick to flush — call stop() which emits a final dataavailable.
      try { recorder.stop() } catch { /* recorder may have stopped already */ }
    }

    try {
      recorder.start()
      src.start()
    } catch {
      finish(null)
      return
    }

    // Safety net in case onended never fires (some Safari builds don't fire
    // it on a buffer-source that never reached its end). Cap at the buffer
    // duration plus a small grace period, bounded by reencodeTimeoutMs.
    const bufferMs = (rendered.length / rendered.sampleRate) * 1000
    const fallbackMs = Math.min(timeoutMs, bufferMs + 500)
    setTimeout(() => {
      if (recorder.state !== 'inactive') {
        try { recorder.stop() } catch { /* swallow */ }
      }
    }, fallbackMs)
    setTimeout(() => finish(null), timeoutMs)
  })
}

export async function trimLeadingTrailingSilence(
  blob: Blob,
  options: TrimOptions = {},
): Promise<Blob> {
  const thresholdAmplitude = options.thresholdAmplitude ?? DEFAULTS.thresholdAmplitude
  const minDurationMs = options.minDurationMs ?? DEFAULTS.minDurationMs
  const windowMs = options.windowMs ?? DEFAULTS.windowMs
  const reencodeTimeoutMs = options.reencodeTimeoutMs ?? DEFAULTS.reencodeTimeoutMs

  const OfflineCtor = getOfflineAudioContextCtor()
  const AudioCtor = getAudioContextCtor()
  if (!OfflineCtor || !AudioCtor) return blob

  try {
    const arrayBuffer = await blob.arrayBuffer()
    const probe = new AudioCtor()
    let decoded: AudioBuffer
    try {
      decoded = await probe.decodeAudioData(arrayBuffer.slice(0))
    } finally {
      void probe.close?.().catch(() => {})
    }

    const sampleRate = decoded.sampleRate
    const windowSamples = Math.max(1, Math.floor(sampleRate * (windowMs / 1000)))
    const { start, end } = findSilenceBoundaries(decoded, thresholdAmplitude, windowSamples)
    const trimmedLength = end - start
    if (trimmedLength <= 0) return blob
    const trimmedDurationMs = (trimmedLength / sampleRate) * 1000
    if (trimmedDurationMs < minDurationMs) return blob
    if (start === 0 && end === decoded.length) return blob

    const channels = decoded.numberOfChannels
    const offline = new OfflineCtor(channels, trimmedLength, sampleRate)
    const trimmedBuffer = offline.createBuffer(channels, trimmedLength, sampleRate)
    for (let c = 0; c < channels; c++) {
      trimmedBuffer.copyToChannel(decoded.getChannelData(c).subarray(start, end), c)
    }
    const src = offline.createBufferSource()
    src.buffer = trimmedBuffer
    src.connect(offline.destination)
    src.start(0)
    const rendered = await offline.startRendering()

    const reencoded = await reencodeTrimmedBuffer(rendered, blob.type || 'audio/webm', reencodeTimeoutMs)
    if (!reencoded || reencoded.size === 0) return blob
    return reencoded
  } catch {
    return blob
  }
}
