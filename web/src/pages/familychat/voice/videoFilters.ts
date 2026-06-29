// videoFilters.ts — real-time local video effects for Family Chat video calls.
//
// The pipeline taps the raw camera track, draws each frame onto a <canvas>
// with an effect applied, and exposes the canvas' captureStream() track. The
// caller (useVoiceCall) replaceTrack()s that processed track onto the existing
// RTCRtpSender so the peer receives the filtered feed and the local PiP shows
// it too — no SDP renegotiation required.
//
// Everything degrades gracefully: createVideoFilterPipeline returns null when
// the browser lacks <canvas>.captureStream, and isFilterSupported() lets the UI
// hide effects the platform can't run (e.g. 'face' needs the experimental
// FaceDetector API). The default 'none' filter never builds a pipeline, so the
// common case stays zero-overhead.

// FilterKind enumerates the effects offered in the in-call filter picker.
//   none    — passthrough, no processing (default, zero cost)
//   blur    — soft-focus full-frame blur
//   grading — warm vintage colour grade
//   face    — sunglasses + party-hat overlay on detected faces (FaceDetector)
export type FilterKind = 'none' | 'blur' | 'grading' | 'face'

// FILTER_KINDS is the display order for the picker. 'none' first so it reads as
// the default/off state.
export const FILTER_KINDS: FilterKind[] = ['none', 'blur', 'grading', 'face']

// Target capture frame rate for the processed track. Capped well below 60 to
// keep CPU/GPU cost reasonable on the Android-only family devices — 24fps is
// smooth enough for a chat call and roughly halves the per-frame work vs 60.
const TARGET_FPS = 24
const FRAME_INTERVAL_MS = 1000 / TARGET_FPS

// Face detection is far more expensive than a plain draw, so we run it at most
// once per this interval and reuse the last-known boxes on intermediate frames.
const FACE_DETECT_INTERVAL_MS = 160

// Default canvas size before the source video reports its real dimensions.
const DEFAULT_WIDTH = 640
const DEFAULT_HEIGHT = 480

// Cache the one-time canvas capability probe.
let canvasSupportCache: boolean | null = null

// canvasFilterSupported reports whether this browser can run a canvas-based
// filter pipeline at all: a 2D context that honours the `filter` property and a
// canvas that can be turned into a MediaStream via captureStream().
export function canvasFilterSupported(): boolean {
  if (canvasSupportCache !== null) return canvasSupportCache
  if (typeof document === 'undefined') {
    canvasSupportCache = false
    return false
  }
  try {
    const canvas = document.createElement('canvas')
    const ctx = canvas.getContext('2d')
    const captureOk = typeof (canvas as HTMLCanvasElement).captureStream === 'function'
    // `filter` is part of the 2D context spec but absent in some older mobile
    // engines; without it blur/grading would silently no-op.
    const filterOk = ctx !== null && 'filter' in ctx
    canvasSupportCache = captureOk && filterOk
  } catch {
    canvasSupportCache = false
  }
  return canvasSupportCache
}

// faceDetectionSupported reports whether the experimental FaceDetector API is
// available. Chrome/Android exposes it behind a flag on some builds; everywhere
// else the 'face' filter is hidden by the UI.
export function faceDetectionSupported(): boolean {
  return typeof window !== 'undefined'
    && 'FaceDetector' in (window as unknown as Record<string, unknown>)
}

// isFilterSupported gates an individual effect. The UI calls this to decide
// which options to render so it never offers a filter the browser can't run.
export function isFilterSupported(kind: FilterKind): boolean {
  switch (kind) {
    case 'none':
      return true
    case 'blur':
    case 'grading':
      return canvasFilterSupported()
    case 'face':
      return canvasFilterSupported() && faceDetectionSupported()
    default:
      return false
  }
}

// supportedFilters returns the effects this browser can actually run, in
// display order. Always includes 'none'.
export function supportedFilters(): FilterKind[] {
  return FILTER_KINDS.filter(isFilterSupported)
}

export interface VideoFilterPipeline {
  // The processed video track to put on the RTCRtpSender and the local preview.
  // Stable for the lifetime of the pipeline — source/filter changes do not
  // replace it, so the sender only needs replaceTrack() once.
  readonly outputTrack: MediaStreamTrack
  // Point the pipeline at a (new) raw camera track. Used on switch-camera /
  // bandwidth re-acquire so the same output track keeps flowing.
  setSource: (track: MediaStreamTrack | null) => void
  // Change the active effect live. Cheap — just flips which CSS filter / overlay
  // the draw loop applies on the next frame.
  setFilter: (filter: FilterKind) => void
  // Stop the draw loop, end the output track, and release the hidden <video>.
  stop: () => void
}

// Minimal structural typings for the experimental FaceDetector API so we can use
// it without a DOM lib that predates it.
interface DetectedFaceBox {
  boundingBox: { x: number; y: number; width: number; height: number }
}
interface FaceDetectorLike {
  detect: (source: CanvasImageSource) => Promise<DetectedFaceBox[]>
}

function cssFilterFor(filter: FilterKind, width: number, height: number): string {
  switch (filter) {
    case 'blur': {
      // Scale the blur radius to the frame so it looks consistent across
      // resolutions (≈2.5% of the shorter edge).
      const radius = Math.max(4, Math.round(Math.min(width, height) * 0.025))
      return `blur(${radius}px)`
    }
    case 'grading':
      return 'sepia(0.55) saturate(1.5) contrast(1.08) brightness(1.05)'
    default:
      return 'none'
  }
}

function drawRoundedRect(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  w: number,
  h: number,
  r: number,
): void {
  const radius = Math.min(r, w / 2, h / 2)
  if (typeof (ctx as unknown as { roundRect?: unknown }).roundRect === 'function') {
    ctx.beginPath()
    ;(ctx as unknown as { roundRect: (x: number, y: number, w: number, h: number, r: number) => void })
      .roundRect(x, y, w, h, radius)
    return
  }
  ctx.beginPath()
  ctx.moveTo(x + radius, y)
  ctx.arcTo(x + w, y, x + w, y + h, radius)
  ctx.arcTo(x + w, y + h, x, y + h, radius)
  ctx.arcTo(x, y + h, x, y, radius)
  ctx.arcTo(x, y, x + w, y, radius)
  ctx.closePath()
}

// Draw sunglasses across the eye band plus a party hat above each detected face.
function drawFaceOverlays(
  ctx: CanvasRenderingContext2D,
  faces: DetectedFaceBox[],
): void {
  for (const face of faces) {
    const { x, y, width, height } = face.boundingBox
    if (!(width > 0) || !(height > 0)) continue

    // Sunglasses: a single rounded band across the upper-third of the face.
    const gw = width * 0.92
    const gh = height * 0.2
    const gx = x + (width - gw) / 2
    const gy = y + height * 0.3
    ctx.fillStyle = '#0b0b0b'
    drawRoundedRect(ctx, gx, gy, gw, gh, gh * 0.5)
    ctx.fill()
    // A thin bridge highlight so it reads as two lenses, not one bar.
    ctx.fillStyle = '#0b0b0b'
    ctx.fillRect(x + width / 2 - gw * 0.03, gy + gh * 0.2, gw * 0.06, gh * 0.6)

    // Party hat: a triangle perched above the forehead.
    ctx.fillStyle = '#e11d48'
    ctx.beginPath()
    ctx.moveTo(x + width / 2, y - height * 0.32)
    ctx.lineTo(x + width * 0.18, y + height * 0.06)
    ctx.lineTo(x + width * 0.82, y + height * 0.06)
    ctx.closePath()
    ctx.fill()
    // Pom-pom on top.
    ctx.fillStyle = '#fde68a'
    ctx.beginPath()
    ctx.arc(x + width / 2, y - height * 0.32, Math.max(3, width * 0.05), 0, Math.PI * 2)
    ctx.fill()
  }
}

// createVideoFilterPipeline builds the canvas processing pipeline. Returns null
// when the browser can't support it so the caller falls back to the raw track.
export function createVideoFilterPipeline(): VideoFilterPipeline | null {
  if (!canvasFilterSupported()) return null
  if (typeof document === 'undefined') return null

  let canvas: HTMLCanvasElement
  let ctx: CanvasRenderingContext2D | null
  let video: HTMLVideoElement
  try {
    canvas = document.createElement('canvas')
    canvas.width = DEFAULT_WIDTH
    canvas.height = DEFAULT_HEIGHT
    ctx = canvas.getContext('2d')
    if (!ctx) return null
    video = document.createElement('video')
    video.muted = true
    video.playsInline = true
    video.autoplay = true
  } catch {
    return null
  }

  const captured = canvas.captureStream(TARGET_FPS)
  const outputTrack = captured.getVideoTracks()[0]
  if (!outputTrack) return null

  let currentFilter: FilterKind = 'none'
  let rafId: number | null = null
  let stopped = false
  let lastDrawAt = 0
  let lastDetectAt = 0
  let detecting = false
  let lastFaces: DetectedFaceBox[] = []

  // Lazily build the FaceDetector only when the face filter is selected so the
  // common path pays nothing.
  let detector: FaceDetectorLike | null = null
  const ensureDetector = (): FaceDetectorLike | null => {
    if (detector) return detector
    if (!faceDetectionSupported()) return null
    try {
      const Ctor = (window as unknown as {
        FaceDetector: new (opts: { fastMode: boolean; maxDetectedFaces: number }) => FaceDetectorLike
      }).FaceDetector
      detector = new Ctor({ fastMode: true, maxDetectedFaces: 5 })
    } catch {
      detector = null
    }
    return detector
  }

  const drawFrame = (): void => {
    if (stopped) return
    rafId = requestAnimationFrame(drawFrame)
    const now = typeof performance !== 'undefined' && typeof performance.now === 'function'
      ? performance.now()
      : Date.now()
    if (now - lastDrawAt < FRAME_INTERVAL_MS) return
    lastDrawAt = now

    if (video.readyState < 2 || video.videoWidth === 0 || video.videoHeight === 0) {
      return
    }
    if (canvas.width !== video.videoWidth || canvas.height !== video.videoHeight) {
      canvas.width = video.videoWidth
      canvas.height = video.videoHeight
    }

    const ctx2 = ctx
    if (!ctx2) return
    ctx2.save()
    ctx2.filter = cssFilterFor(currentFilter, canvas.width, canvas.height)
    ctx2.drawImage(video, 0, 0, canvas.width, canvas.height)
    ctx2.restore()

    if (currentFilter === 'face') {
      const det = ensureDetector()
      if (det && !detecting && now - lastDetectAt >= FACE_DETECT_INTERVAL_MS) {
        detecting = true
        lastDetectAt = now
        // Fire-and-forget: detection runs off the draw loop and updates the
        // cached boxes for subsequent frames. A failure just clears overlays.
        det.detect(canvas)
          .then(faces => { lastFaces = Array.isArray(faces) ? faces : [] })
          .catch(() => { lastFaces = [] })
          .finally(() => { detecting = false })
      }
      drawFaceOverlays(ctx2, lastFaces)
    } else if (lastFaces.length > 0) {
      // Leaving the face filter — drop stale boxes so they don't reappear.
      lastFaces = []
    }
  }

  const startLoop = (): void => {
    if (rafId !== null) return
    if (typeof requestAnimationFrame !== 'function') return
    rafId = requestAnimationFrame(drawFrame)
  }

  const setSource = (track: MediaStreamTrack | null): void => {
    if (stopped) return
    if (!track) {
      video.srcObject = null
      return
    }
    try {
      video.srcObject = new MediaStream([track])
      void video.play().catch(() => { /* autoplay policy — loop tolerates a not-yet-playing video */ })
    } catch {
      /* setting srcObject can throw in exotic engines; the loop simply idles */
    }
    startLoop()
  }

  const setFilter = (filter: FilterKind): void => {
    currentFilter = filter
  }

  const stop = (): void => {
    if (stopped) return
    stopped = true
    if (rafId !== null && typeof cancelAnimationFrame === 'function') {
      cancelAnimationFrame(rafId)
    }
    rafId = null
    try { video.pause() } catch { /* ignore */ }
    video.srcObject = null
    try { outputTrack.stop() } catch { /* already stopped */ }
  }

  return {
    outputTrack,
    setSource,
    setFilter,
    stop,
  }
}
