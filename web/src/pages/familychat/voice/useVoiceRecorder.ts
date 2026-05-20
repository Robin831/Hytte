import { useCallback, useEffect, useRef, useState } from 'react'

// useVoiceRecorder wraps MediaRecorder for short Family Chat voice notes.
// The hook is intentionally UI-agnostic: it exposes state, an amplitude meter
// (10 rolling bars), elapsed/remaining time and start/stop/cancel actions.
// The consuming component decides between press-and-hold (touch) and
// click-toggle (desktop) and feeds pointer movement to setPointerDelta so the
// swipe-up cancel gesture can arm/disarm without the hook owning DOM events.

export type RecorderState = 'idle' | 'starting' | 'recording' | 'processing' | 'error'

export interface RecorderResult {
  blob: Blob
  mimeType: string
  durationMs: number
}

export interface UseVoiceRecorderOptions {
  maxDurationMs?: number
  preferredMimeType?: string
  barCount?: number
  cancelThresholdPx?: number
  minDurationMs?: number
  // Fired when the recorder stops itself because maxDurationMs was hit. The
  // consumer would not have called stop() in that case, so they get no
  // promise resolution from the user-initiated path — this callback closes
  // that gap so the cap can still trigger a commit.
  onAutoComplete?: (result: RecorderResult | null) => void
}

export interface UseVoiceRecorderApi {
  state: RecorderState
  supported: boolean
  error: string | null
  elapsedMs: number
  remainingMs: number
  maxDurationMs: number
  levels: number[]
  cancelArmed: boolean
  start: () => Promise<void>
  stop: () => Promise<RecorderResult | null>
  cancel: () => void
  setPointerDelta: (deltaY: number) => void
  resetPointer: () => void
}

const DEFAULT_MAX_DURATION_MS = 30000
const DEFAULT_PREFERRED_MIME = 'audio/webm'
const DEFAULT_BAR_COUNT = 10
const DEFAULT_CANCEL_THRESHOLD_PX = 80
const DEFAULT_MIN_DURATION_MS = 350

// Browsers disagree on which MediaRecorder MIME they emit. Chromium prefers
// audio/webm;codecs=opus; Firefox emits audio/ogg;codecs=opus. The backend
// accepts both — pick the first the platform supports so we never hand the
// recorder an option it would reject (which would yield a "TypeError" instead
// of a usable stream).
function pickSupportedMime(preferred: string): string {
  if (typeof MediaRecorder === 'undefined') return preferred
  const isSupported = typeof MediaRecorder.isTypeSupported === 'function'
    ? (t: string) => MediaRecorder.isTypeSupported(t)
    : () => false
  const candidates = [
    preferred,
    'audio/webm;codecs=opus',
    'audio/webm',
    'audio/ogg;codecs=opus',
    'audio/ogg',
  ]
  for (const c of candidates) {
    if (isSupported(c)) return c
  }
  return preferred
}

function isMediaRecorderAvailable(): boolean {
  return typeof window !== 'undefined' && typeof window.MediaRecorder !== 'undefined'
}

function isGetUserMediaAvailable(): boolean {
  return typeof navigator !== 'undefined'
    && !!navigator.mediaDevices
    && typeof navigator.mediaDevices.getUserMedia === 'function'
}

export function useVoiceRecorder(options: UseVoiceRecorderOptions = {}): UseVoiceRecorderApi {
  const maxDurationMs = options.maxDurationMs ?? DEFAULT_MAX_DURATION_MS
  const preferredMimeType = options.preferredMimeType ?? DEFAULT_PREFERRED_MIME
  const barCount = options.barCount ?? DEFAULT_BAR_COUNT
  const cancelThresholdPx = options.cancelThresholdPx ?? DEFAULT_CANCEL_THRESHOLD_PX
  const minDurationMs = options.minDurationMs ?? DEFAULT_MIN_DURATION_MS

  const [state, setState] = useState<RecorderState>('idle')
  const [error, setError] = useState<string | null>(null)
  const [elapsedMs, setElapsedMs] = useState(0)
  const [cancelArmed, setCancelArmed] = useState(false)
  const [levels, setLevels] = useState<number[]>(() => new Array(barCount).fill(0))

  const streamRef = useRef<MediaStream | null>(null)
  const recorderRef = useRef<MediaRecorder | null>(null)
  const chunksRef = useRef<BlobPart[]>([])
  const audioCtxRef = useRef<AudioContext | null>(null)
  const analyserRef = useRef<AnalyserNode | null>(null)
  const sourceRef = useRef<MediaStreamAudioSourceNode | null>(null)
  const dataArrayRef = useRef<Uint8Array<ArrayBuffer> | null>(null)
  const rafRef = useRef<number | null>(null)
  const lastMeterTimeRef = useRef<number>(0)
  const tickRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const maxTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const startedAtRef = useRef<number>(0)
  const finalDurationRef = useRef<number>(0)
  const cancelledRef = useRef<boolean>(false)
  const finalisingRef = useRef<boolean>(false)
  const stopPromiseRef = useRef<{
    resolve: (v: RecorderResult | null) => void
  } | null>(null)
  const mimeTypeRef = useRef<string>(preferredMimeType)
  const levelsHistoryRef = useRef<number[]>(new Array(barCount).fill(0))
  const autoCompleteRef = useRef<boolean>(false)
  const onAutoCompleteRef = useRef<UseVoiceRecorderOptions['onAutoComplete']>(options.onAutoComplete)
  // Synchronous guard so a rapid second start() call in the same tick
  // (e.g. double pointerdown) is dropped before any async work begins.
  // State-based checks are async and cannot catch this race.
  const startInFlightRef = useRef(false)
  useEffect(() => { onAutoCompleteRef.current = options.onAutoComplete })

  // Sync levelsHistoryRef when barCount changes (rare). The levels state is
  // reset in start() before each recording, so it self-corrects on next use.
  useEffect(() => {
    levelsHistoryRef.current = new Array(barCount).fill(0)
  }, [barCount])

  const supported = isGetUserMediaAvailable() && isMediaRecorderAvailable()

  const clearAnimation = useCallback(() => {
    if (rafRef.current !== null) {
      cancelAnimationFrame(rafRef.current)
      rafRef.current = null
    }
  }, [])

  const clearTimers = useCallback(() => {
    if (tickRef.current !== null) {
      clearInterval(tickRef.current)
      tickRef.current = null
    }
    if (maxTimerRef.current !== null) {
      clearTimeout(maxTimerRef.current)
      maxTimerRef.current = null
    }
  }, [])

  const releaseAudioGraph = useCallback(() => {
    clearAnimation()
    try { sourceRef.current?.disconnect() } catch { /* already disconnected */ }
    sourceRef.current = null
    analyserRef.current = null
    dataArrayRef.current = null
    const ctx = audioCtxRef.current
    audioCtxRef.current = null
    if (ctx && typeof ctx.close === 'function') {
      // close() is async but we don't need to await — the AudioContext drops
      // its references once close resolves and there are no other strong refs.
      void ctx.close().catch(() => {})
    }
  }, [clearAnimation])

  const releaseStream = useCallback(() => {
    const stream = streamRef.current
    streamRef.current = null
    if (stream) {
      for (const track of stream.getTracks()) {
        try { track.stop() } catch { /* track already stopped */ }
      }
    }
  }, [])

  const fullTeardown = useCallback(() => {
    clearTimers()
    releaseAudioGraph()
    releaseStream()
    recorderRef.current = null
    chunksRef.current = []
  }, [clearTimers, releaseAudioGraph, releaseStream])

  // Tear down on unmount so an in-flight recording doesn't keep the mic LED on
  // when the user navigates away mid-record.
  useEffect(() => {
    return () => {
      cancelledRef.current = true
      fullTeardown()
      const pending = stopPromiseRef.current
      stopPromiseRef.current = null
      pending?.resolve(null)
    }
  }, [fullTeardown])

  const sampleAmplitude = useCallback(() => {
    const analyser = analyserRef.current
    const data = dataArrayRef.current
    if (!analyser || !data) return
    // Throttle to ~15 fps so the meter doesn't force a React re-render on every
    // animation frame (~60fps), which causes jank on low-end devices.
    const now = performance.now()
    if (now - lastMeterTimeRef.current < 66) return
    lastMeterTimeRef.current = now
    analyser.getByteTimeDomainData(data)
    // RMS over the time-domain buffer. getByteTimeDomainData yields uint8
    // values centred on 128; subtract the bias before squaring.
    let sumSq = 0
    for (let i = 0; i < data.length; i++) {
      const v = (data[i] - 128) / 128
      sumSq += v * v
    }
    const rms = Math.sqrt(sumSq / data.length)
    // Compress the dynamic range so quiet speech still produces visible bars.
    const level = Math.min(1, rms * 4)
    const history = levelsHistoryRef.current
    history.shift()
    history.push(level)
    setLevels([...history])
  }, [])

  // startMeterLoop schedules a self-rescheduling RAF loop that samples the
  // analyser node every frame. The loop terminates when clearAnimation() is
  // called (cancel/stop) or when the analyser is released — sampleAmplitude
  // bails on a missing analyser so a late frame from a torn-down session is a
  // no-op.
  const startMeterLoop = useCallback(() => {
    const tick = () => {
      if (!analyserRef.current || !dataArrayRef.current) return
      sampleAmplitude()
      rafRef.current = requestAnimationFrame(tick)
    }
    rafRef.current = requestAnimationFrame(tick)
  }, [sampleAmplitude])

  // We resolve the stop() promise lazily after MediaRecorder.onstop fires
  // (chunks arrive via ondataavailable then onstop). The handler reads from
  // refs so it doesn't capture stale state from the start() closure.
  const finaliseStop = useCallback(() => {
    if (finalisingRef.current) return
    finalisingRef.current = true
    clearTimers()
    clearAnimation()

    const cancelled = cancelledRef.current
    const mime = mimeTypeRef.current
    const durationMs = finalDurationRef.current

    releaseAudioGraph()
    releaseStream()
    recorderRef.current = null

    const chunks = chunksRef.current
    chunksRef.current = []

    const pending = stopPromiseRef.current
    stopPromiseRef.current = null

    const autoComplete = autoCompleteRef.current
    autoCompleteRef.current = false

    if (cancelled) {
      setState('idle')
      pending?.resolve(null)
      if (autoComplete) onAutoCompleteRef.current?.(null)
      return
    }

    if (chunks.length === 0 || durationMs < minDurationMs) {
      setState('idle')
      pending?.resolve(null)
      if (autoComplete) onAutoCompleteRef.current?.(null)
      return
    }

    const blob = new Blob(chunks, { type: mime })
    const result: RecorderResult = { blob, mimeType: mime, durationMs }
    setState('idle')
    pending?.resolve(result)
    if (autoComplete) onAutoCompleteRef.current?.(result)
  }, [
    clearAnimation,
    clearTimers,
    minDurationMs,
    releaseAudioGraph,
    releaseStream,
  ])

  const start = useCallback(async () => {
    if (startInFlightRef.current) return
    if (state === 'recording' || state === 'starting') return
    startInFlightRef.current = true
    try {
    if (!supported) {
      setError('unsupported')
      setState('error')
      return
    }
    setError(null)
    setCancelArmed(false)
    setElapsedMs(0)
    levelsHistoryRef.current = new Array(barCount).fill(0)
    setLevels(new Array(barCount).fill(0))
    cancelledRef.current = false
    finalisingRef.current = false
    chunksRef.current = []
    setState('starting')

    let stream: MediaStream
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true })
    } catch (err) {
      const denied = err instanceof DOMException
        && (err.name === 'NotAllowedError' || err.name === 'PermissionDeniedError')
      setError(denied ? 'permission' : 'unavailable')
      setState('error')
      return
    }
    // cancel() may have been called while getUserMedia was in flight (e.g. on
    // conversation switch or unmount). Stop the obtained tracks and bail before
    // any recorder state is created so the mic LED goes off immediately.
    if (cancelledRef.current) {
      for (const track of stream.getTracks()) {
        try { track.stop() } catch { /* already stopped */ }
      }
      setState('idle')
      return
    }
    streamRef.current = stream

    const mime = pickSupportedMime(preferredMimeType)
    mimeTypeRef.current = mime

    let recorder: MediaRecorder
    try {
      recorder = mime
        ? new MediaRecorder(stream, { mimeType: mime })
        : new MediaRecorder(stream)
    } catch {
      // Fall back to the default constructor if the browser rejects our mime.
      try {
        recorder = new MediaRecorder(stream)
        mimeTypeRef.current = recorder.mimeType || mime
      } catch {
        releaseStream()
        setError('unsupported')
        setState('error')
        return
      }
    }
    recorderRef.current = recorder

    recorder.ondataavailable = (event: BlobEvent) => {
      if (event.data && event.data.size > 0) {
        chunksRef.current.push(event.data)
      }
    }
    recorder.onerror = () => {
      cancelledRef.current = true
      setError('recorder')
      finalDurationRef.current = Date.now() - startedAtRef.current
      try { recorder.stop() } catch { /* recorder may already be inactive */ }
    }
    recorder.onstop = () => {
      finaliseStop()
    }

    // Set up the analyser so the bars can react. Failure here is non-fatal —
    // we still record audio, the meter just stays flat. happy-dom and older
    // Safari versions are the typical sources of a missing AudioContext.
    try {
      const Ctor = window.AudioContext
        ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
      if (Ctor) {
        const ctx = new Ctor()
        audioCtxRef.current = ctx
        const source = ctx.createMediaStreamSource(stream)
        sourceRef.current = source
        const analyser = ctx.createAnalyser()
        analyser.fftSize = 1024
        analyser.smoothingTimeConstant = 0.6
        source.connect(analyser)
        analyserRef.current = analyser
        dataArrayRef.current = new Uint8Array(analyser.fftSize)
      }
    } catch {
      releaseAudioGraph()
    }

    startedAtRef.current = Date.now()
    finalDurationRef.current = 0
    try {
      recorder.start()
    } catch {
      releaseStream()
      releaseAudioGraph()
      setError('unsupported')
      setState('error')
      return
    }

    setState('recording')

    tickRef.current = setInterval(() => {
      const elapsed = Date.now() - startedAtRef.current
      setElapsedMs(Math.min(elapsed, maxDurationMs))
    }, 100)

    if (analyserRef.current) {
      startMeterLoop()
    }

    maxTimerRef.current = setTimeout(() => {
      // Auto-stop on cap. Mark non-cancelled so the chunks ship as a normal
      // result. The autoComplete flag tells finaliseStop to deliver the blob
      // through onAutoComplete since the consumer didn't call stop().
      cancelledRef.current = false
      autoCompleteRef.current = true
      finalDurationRef.current = Date.now() - startedAtRef.current
      setState('processing')
      try { recorder.stop() } catch { /* recorder may have stopped already */ }
    }, maxDurationMs)
    } finally {
      startInFlightRef.current = false
    }
  }, [
    barCount,
    finaliseStop,
    maxDurationMs,
    preferredMimeType,
    releaseAudioGraph,
    releaseStream,
    startMeterLoop,
    state,
    supported,
  ])

  const stop = useCallback((): Promise<RecorderResult | null> => {
    const recorder = recorderRef.current
    if (!recorder || (state !== 'recording' && state !== 'starting')) {
      return Promise.resolve(null)
    }
    // Guard against rapid double-calls before React re-renders state.
    if (stopPromiseRef.current !== null) {
      return Promise.resolve(null)
    }
    // Clear the max-duration timer so it cannot race with this user-initiated
    // stop — without this, a timeout firing between stop() and onstop would
    // set autoCompleteRef and cause finaliseStop to invoke onAutoComplete in
    // addition to resolving the stop() promise (double-commit bug).
    if (maxTimerRef.current !== null) {
      clearTimeout(maxTimerRef.current)
      maxTimerRef.current = null
    }
    autoCompleteRef.current = false
    finalDurationRef.current = Date.now() - startedAtRef.current
    cancelledRef.current = false
    setState('processing')
    return new Promise<RecorderResult | null>(resolve => {
      stopPromiseRef.current = { resolve }
      try {
        recorder.stop()
      } catch {
        // Treat a stop() throw as success-with-no-data: finaliseStop will run
        // either via onstop (already fired) or via the safety branch below.
        finaliseStop()
      }
    })
  }, [finaliseStop, state])

  const cancel = useCallback(() => {
    const recorder = recorderRef.current
    cancelledRef.current = true
    // Clear the max-duration timer so it cannot fire after cancel begins.
    if (maxTimerRef.current !== null) {
      clearTimeout(maxTimerRef.current)
      maxTimerRef.current = null
    }
    autoCompleteRef.current = false
    if (!recorder) {
      // Nothing to stop — just drop any latent error/state and tear down.
      fullTeardown()
      setState('idle')
      return
    }
    finalDurationRef.current = Date.now() - startedAtRef.current
    setState('processing')
    try {
      recorder.stop()
    } catch {
      finaliseStop()
    }
  }, [finaliseStop, fullTeardown])

  const setPointerDelta = useCallback((deltaY: number) => {
    // deltaY is the pointer's vertical displacement since pointerdown,
    // negative when the pointer moves up (clientY − startY). The sign is
    // negated internally to get the upward distance, so callers must pass
    // the raw signed delta — not an absolute value.
    const upward = -deltaY
    setCancelArmed(upward >= cancelThresholdPx)
  }, [cancelThresholdPx])

  const resetPointer = useCallback(() => {
    setCancelArmed(false)
  }, [])

  const remainingMs = Math.max(0, maxDurationMs - elapsedMs)

  return {
    state,
    supported,
    error,
    elapsedMs,
    remainingMs,
    maxDurationMs,
    levels,
    cancelArmed,
    start,
    stop,
    cancel,
    setPointerDelta,
    resetPointer,
  }
}
