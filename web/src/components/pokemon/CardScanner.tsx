import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Camera, Flashlight, FlashlightOff, Loader2, X } from 'lucide-react'
import {
  detectCardRectangle,
  isWithinTolerance,
  type DetectedRectangle,
  type RectangleDetectorStatus,
} from './rectangleDetector'
import ToastList from '../ToastList'
import { useToast } from '../../hooks/useToast'

export interface CardScannerProps {
  onClose: () => void
  onAdded?: () => void
}

type PermissionState = 'prompting' | 'granted' | 'denied' | 'unavailable' | 'unsupported'

// scanPhase tracks the lifecycle of the scan POST flow, layered on top
// of the rectangle-detector state machine. idle = no submission in flight;
// preview = captured frame on-screen awaiting user confirm or auto-send;
// submitting = POST to /api/pokemon/scans/queue pending;
// cooldown = post-queue debounce window during which auto-triggered scans
// are suppressed so the same card sitting in the frame doesn't double-queue.
type ScanPhase = 'idle' | 'preview' | 'submitting' | 'cooldown'

// Throttle the auto-detect tick to ~2/sec. Two consecutive matching ticks
// (~1s of stable framing) promote a candidate to `locked`.
const DETECT_TICK_MS = 500
const TICKS_TO_LOCK = 2
// Allow up to ±5% drift between consecutive candidate detections.
const CANDIDATE_TOLERANCE = 0.05

// SCAN_TIMEOUT_MS is the hard cap on the /api/pokemon/scans/queue POST.
// The queue endpoint just persists the upload and returns 202, so 30 s is
// plenty — anything longer suggests a network or server problem.
const SCAN_TIMEOUT_MS = 30000

// COOLDOWN_MS is the debounce window after a successful queue. The rAF loop
// will not trigger another POST until this window elapses, so a card
// lingering in the frame cannot double-queue while the user is moving on.
const COOLDOWN_MS = 2000

// QUEUED_TOAST_MS is how long the inline "Sent ✓" toast lingers over the
// scanner overlay after a successful queue. Short enough to stay out of the
// way while the kid moves on to the next card, long enough to register.
const QUEUED_TOAST_MS = 1500

// PREVIEW_AUTO_SEND_MS is how long the captured-card preview lingers before
// automatically proceeding to POST. Kids tend to keep scanning rather than
// interact, so default-proceed prevents friction; Retake gives a quick out
// when the capture was bad.
const PREVIEW_AUTO_SEND_MS = 1500

// CAPTURE_CROP_PAD is the fraction of the detected card bounds added on each
// axis before cropping. Keeps the card border / set symbol / collector number
// from being clipped if the detection rectangle hugs the card too tightly.
const CAPTURE_CROP_PAD = 0.05

// Pokémon TCG cards are 63x88mm — aspect ratio ≈ 0.716. The guide overlay
// uses 5/7 (≈0.714) which is close enough and renders crisply on all viewports.
const CARD_GUIDE_ASPECT = '5 / 7'

interface ExtendedMediaTrackCapabilities extends MediaTrackCapabilities {
  torch?: boolean
}

interface TorchConstraint extends MediaTrackConstraintSet {
  torch: boolean
}

interface ExtendedMediaTrackConstraints extends MediaTrackConstraints {
  advanced?: TorchConstraint[]
}

export default function CardScanner({ onClose, onAdded }: CardScannerProps) {
  const { t } = useTranslation('pokemon')
  const { toasts, showToast } = useToast()

  const [permissionState, setPermissionState] = useState<PermissionState>(() =>
    typeof navigator.mediaDevices?.getUserMedia === 'function' ? 'prompting' : 'unsupported',
  )
  const [torchOn, setTorchOn] = useState(false)
  const [torchSupported, setTorchSupported] = useState(false)
  const [scanStatus, setScanStatus] = useState<RectangleDetectorStatus>('searching')
  const [scanPhase, setScanPhase] = useState<ScanPhase>('idle')
  const [previewUrl, setPreviewUrl] = useState<string | null>(null)
  // dailyLimitReached freezes the scanner when the queue endpoint returns 429.
  // The auto-detect loop and shutter both refuse to fire while this is true;
  // the kid has to close + come back tomorrow (or ask Robin to lift the cap).
  const [dailyLimitReached, setDailyLimitReached] = useState(false)
  // queuedToastVisible drives the inline "Sent ✓ — view in Scanned" banner.
  // It's a separate piece of UI (not a global toast) so it can carry a link
  // to /pokemon/scanned without going through the ToastList plumbing.
  const [queuedToastVisible, setQueuedToastVisible] = useState(false)

  const videoRef = useRef<HTMLVideoElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const streamRef = useRef<MediaStream | null>(null)
  const closeButtonRef = useRef<HTMLButtonElement>(null)
  const dialogRef = useRef<HTMLDivElement>(null)

  // Auto-detect refs — kept outside React state so the rAF loop reads the
  // freshest values without triggering re-renders on every tick.
  const detectCanvasRef = useRef<HTMLCanvasElement | null>(null)
  const scanStatusRef = useRef<RectangleDetectorStatus>('searching')
  const candidateBoundsRef = useRef<DetectedRectangle | null>(null)
  const candidateTicksRef = useRef(0)
  const lastTickRef = useRef(0)
  const rafIdRef = useRef<number | null>(null)
  // Allows captureLocked's failure recovery path to restart the rAF loop
  // without a circular const dependency on `tick`.
  const tickFnRef = useRef<FrameRequestCallback | null>(null)
  // scanPhaseRef mirrors scanPhase so the rAF callback can read the freshest
  // value without re-creating itself on every phase transition.
  const scanPhaseRef = useRef<ScanPhase>('idle')
  // cooldownUntilRef holds the timestamp (Date.now()) until which auto scans
  // are suppressed. Both manual and auto callers consult it; manual scans
  // intentionally bypass this check.
  const cooldownUntilRef = useRef(0)
  // scanAbortRef is used to abort the in-flight scan when the component
  // unmounts mid-submission.
  const scanAbortRef = useRef<AbortController | null>(null)
  // Track the latest performScan callback in a ref so the rAF loop always
  // calls the current version without needing to be re-installed.
  const performScanRef = useRef<(blob: Blob, manual: boolean) => void>(() => {})
  // cooldownTimerRef holds the id of the pending timer that transitions
  // scanPhase from 'cooldown' back to 'idle'. Keeping it in a ref lets us
  // cancel it on unmount or close so the callback never fires on a dead component.
  const cooldownTimerRef = useRef<ReturnType<typeof window.setTimeout> | null>(null)
  // queuedToastTimerRef hides the inline "Sent ✓" banner after QUEUED_TOAST_MS.
  // Stored in a ref so unmount can cancel it and the timer's setter doesn't
  // run after the component has been torn down.
  const queuedToastTimerRef = useRef<ReturnType<typeof window.setTimeout> | null>(null)
  // dailyLimitReachedRef mirrors dailyLimitReached so the rAF callback can
  // halt itself without forcing the effect to re-install on state change.
  const dailyLimitReachedRef = useRef(false)
  // lockedBoundsRef captures the detected card rectangle (in source-canvas /
  // full-video-resolution coordinates) at the moment the rAF loop transitions
  // to `locked`. The capture step uses these bounds to crop the JPEG so only
  // the card itself — not the kid's hand, the table, or glare — is sent to
  // Claude vision.
  const lockedBoundsRef = useRef<DetectedRectangle | null>(null)
  // Preview state: the blob is held in a ref so the auto-send timer can read
  // it without depending on stale closure state, and the object URL is tracked
  // separately so revocation is guaranteed even if the rendered URL state has
  // already cleared.
  const previewBlobRef = useRef<Blob | null>(null)
  const previewWasManualRef = useRef<boolean>(false)
  const previewUrlRef = useRef<string | null>(null)
  const previewTimerRef = useRef<ReturnType<typeof window.setTimeout> | null>(null)

  // Acquire the camera on mount; stop tracks on unmount so the camera LED
  // turns off even if the parent unmounts the scanner without calling onClose.
  useEffect(() => {
    let cancelled = false

    if (!navigator.mediaDevices?.getUserMedia) return

    void (async () => {
      try {
        // Request 1080p so the cropped card (post-detection) has enough pixels
        // for Claude vision to read the set symbol and collector number. The
        // browser falls back to the closest available preset automatically.
        const stream = await navigator.mediaDevices.getUserMedia({
          video: {
            facingMode: { ideal: 'environment' },
            width: { ideal: 1920 },
            height: { ideal: 1080 },
          },
        })
        if (cancelled) {
          stream.getTracks().forEach(track => track.stop())
          return
        }
        streamRef.current = stream
        // Note: the <video> element is only rendered after permissionState
        // flips to 'granted', so videoRef.current is null at this point.
        // A separate effect attaches srcObject once the element mounts.
        const [track] = stream.getVideoTracks()
        if (track && typeof track.getCapabilities === 'function') {
          const caps = track.getCapabilities() as ExtendedMediaTrackCapabilities
          if (caps.torch === true) setTorchSupported(true)
        }
        setPermissionState('granted')
      } catch (err) {
        if (cancelled) return
        const isDenied =
          err instanceof DOMException &&
          (err.name === 'NotAllowedError' || err.name === 'PermissionDeniedError')
        setPermissionState(isDenied ? 'denied' : 'unavailable')
      }
    })()

    return () => {
      cancelled = true
      const stream = streamRef.current
      if (stream) {
        stream.getTracks().forEach(track => track.stop())
        streamRef.current = null
      }
      const ctl = scanAbortRef.current
      if (ctl) {
        ctl.abort()
        scanAbortRef.current = null
      }
      if (cooldownTimerRef.current !== null) {
        window.clearTimeout(cooldownTimerRef.current)
        cooldownTimerRef.current = null
      }
      if (queuedToastTimerRef.current !== null) {
        window.clearTimeout(queuedToastTimerRef.current)
        queuedToastTimerRef.current = null
      }
      if (previewTimerRef.current !== null) {
        window.clearTimeout(previewTimerRef.current)
        previewTimerRef.current = null
      }
      if (previewUrlRef.current) {
        URL.revokeObjectURL(previewUrlRef.current)
        previewUrlRef.current = null
      }
      previewBlobRef.current = null
    }
  }, [])

  // Attach the captured stream to the <video> element once permissionState
  // flips to 'granted' and the element actually exists in the DOM. Without
  // this, srcObject would be assigned before the conditional <video> rendered,
  // and the camera preview would stay black even though the stream is live.
  // iOS Safari in particular needs this; it won't reattach implicitly.
  useEffect(() => {
    if (permissionState !== 'granted') return
    const video = videoRef.current
    const stream = streamRef.current
    if (!video || !stream) return
    if (video.srcObject !== stream) {
      video.srcObject = stream
    }
    // Defensive: explicit play() in case autoplay doesn't kick in (some
    // browsers under power-save / low-power mode).
    if (typeof video.play === 'function') {
      void video.play().catch(() => {})
    }
  }, [permissionState])

  // clearPreview tears down the preview overlay: revokes the object URL,
  // clears the auto-send timer, and drops the cached blob. Used by Retake,
  // resumeScanning, unmount, and close.
  const clearPreview = useCallback(() => {
    if (previewTimerRef.current !== null) {
      window.clearTimeout(previewTimerRef.current)
      previewTimerRef.current = null
    }
    if (previewUrlRef.current) {
      URL.revokeObjectURL(previewUrlRef.current)
      previewUrlRef.current = null
    }
    previewBlobRef.current = null
    previewWasManualRef.current = false
    setPreviewUrl(null)
  }, [])

  // resumeScanning resets all refs and React state to the initial scanning
  // posture: clears the detector candidates, lifts the captured/locked freeze,
  // and restarts video playback + the rAF loop. Called after timeout/error,
  // Try Again, or when the cooldown elapses post-add. The rAF restart cancels
  // any stale id first because the captureLocked success path leaves the ref
  // pointing at the last-scheduled callback without a live rAF in the queue.
  const resumeScanning = useCallback(() => {
    scanStatusRef.current = 'searching'
    candidateBoundsRef.current = null
    candidateTicksRef.current = 0
    lastTickRef.current = 0
    lockedBoundsRef.current = null
    clearPreview()
    setScanStatus('searching')
    const video = videoRef.current
    if (video && typeof video.play === 'function') {
      try {
        void video.play()
      } catch {
        // best-effort — pause/play can throw on some mobile browsers
      }
    }
    if (typeof window !== 'undefined' && tickFnRef.current) {
      if (rafIdRef.current !== null) {
        window.cancelAnimationFrame(rafIdRef.current)
      }
      rafIdRef.current = window.requestAnimationFrame(tickFnRef.current)
    }
  }, [clearPreview])

  // performScan is the single entry point for both auto- and manual-triggered
  // scans. It POSTs the JPEG to /api/pokemon/scans/queue and immediately
  // resumes scanning on 202 — no waiting for Claude vision. The Scanned page
  // is where the kid (or a parent) resolves matched/no_match/failed jobs.
  // 429 freezes the scanner until close so the kid stops auto-firing into
  // the daily cap.
  const performScan = useCallback(
    (blob: Blob, manual: boolean) => {
      if (scanPhaseRef.current !== 'idle' && scanPhaseRef.current !== 'cooldown') return
      if (dailyLimitReachedRef.current) return
      // Auto-triggered scans honor the post-queue cooldown. The manual shutter
      // button intentionally bypasses both the lock requirement and the
      // cooldown — the user is asking explicitly.
      if (!manual && Date.now() < cooldownUntilRef.current) {
        resumeScanning()
        return
      }

      scanPhaseRef.current = 'submitting'
      setScanPhase('submitting')

      const controller = new AbortController()
      scanAbortRef.current = controller
      // timedOut distinguishes the timeout fire from a programmatic abort
      // (e.g. component unmount). Only the timeout case surfaces a toast.
      let timedOut = false
      const timer = window.setTimeout(() => {
        timedOut = true
        controller.abort()
      }, SCAN_TIMEOUT_MS)

      const formData = new FormData()
      formData.append('image', blob, 'card.jpg')

      void (async () => {
        try {
          const queueRes = await fetch('/api/pokemon/scans/queue', {
            method: 'POST',
            credentials: 'include',
            body: formData,
            signal: controller.signal,
          })
          if (queueRes.status === 429) {
            dailyLimitReachedRef.current = true
            setDailyLimitReached(true)
            scanPhaseRef.current = 'idle'
            setScanPhase('idle')
            return
          }
          if (!queueRes.ok) throw new Error(t('scanner.errors.scanFailed'))

          // Drain the body so the connection can be reused — we don't read
          // any fields off the response (id, dedupe header) because the
          // fire-and-forget flow doesn't need them.
          try { await queueRes.json() } catch { /* ignore empty/invalid body */ }

          onAdded?.()
          // Show the inline "Sent ✓ — view in Scanned" toast over the
          // scanner overlay so the kid sees an acknowledgement without the
          // scanner ever leaving the searching state.
          if (queuedToastTimerRef.current !== null) {
            window.clearTimeout(queuedToastTimerRef.current)
          }
          setQueuedToastVisible(true)
          queuedToastTimerRef.current = window.setTimeout(() => {
            queuedToastTimerRef.current = null
            setQueuedToastVisible(false)
          }, QUEUED_TOAST_MS)

          // Start the 2 s cooldown so the auto-detector doesn't immediately
          // re-fire on the same card sitting in the frame. Cooldown is
          // enforced both at trigger-time (in the rAF) and inside performScan.
          cooldownUntilRef.current = Date.now() + COOLDOWN_MS
          scanPhaseRef.current = 'cooldown'
          setScanPhase('cooldown')
          resumeScanning()
          if (cooldownTimerRef.current !== null) {
            window.clearTimeout(cooldownTimerRef.current)
          }
          cooldownTimerRef.current = window.setTimeout(() => {
            cooldownTimerRef.current = null
            if (scanPhaseRef.current === 'cooldown') {
              scanPhaseRef.current = 'idle'
              setScanPhase('idle')
            }
          }, COOLDOWN_MS)
        } catch (err) {
          if (controller.signal.aborted) {
            if (timedOut) {
              showToast(t('scanner.errors.timedOut'), 'error')
              scanPhaseRef.current = 'idle'
              setScanPhase('idle')
              resumeScanning()
            }
            // If aborted without timing out (component unmount), the cleanup
            // already tore everything down — do nothing.
            return
          }
          showToast(
            err instanceof Error ? err.message : t('scanner.errors.scanFailed'),
            'error',
          )
          scanPhaseRef.current = 'idle'
          setScanPhase('idle')
          resumeScanning()
        } finally {
          window.clearTimeout(timer)
          if (scanAbortRef.current === controller) scanAbortRef.current = null
        }
      })()
    },
    [onAdded, resumeScanning, showToast, t],
  )

  useEffect(() => {
    performScanRef.current = performScan
  }, [performScan])

  // sendPreview is the "proceed" path out of preview: clears the timer, drops
  // the overlay, and forwards the cached blob to performScan. Reads the cached
  // manual flag so a shutter-triggered preview still bypasses the cooldown.
  // Resets scanPhaseRef to 'idle' before delegating because performScan's own
  // guard refuses to run from any other state.
  const sendPreview = useCallback(() => {
    // Guard against double-invocation (double-tap or timer + button race): if
    // the phase has already moved on, this call is a stale duplicate — drop it.
    if (scanPhaseRef.current !== 'preview') return
    if (previewTimerRef.current !== null) {
      window.clearTimeout(previewTimerRef.current)
      previewTimerRef.current = null
    }
    const blob = previewBlobRef.current
    const manual = previewWasManualRef.current
    if (previewUrlRef.current) {
      URL.revokeObjectURL(previewUrlRef.current)
      previewUrlRef.current = null
    }
    previewBlobRef.current = null
    previewWasManualRef.current = false
    setPreviewUrl(null)
    scanPhaseRef.current = 'idle'
    setScanPhase('idle')
    if (!blob) {
      // Defensive — should not happen, but if it does, drop back to scanning.
      resumeScanning()
      return
    }
    performScanRef.current(blob, manual)
  }, [resumeScanning])

  const sendPreviewRef = useRef<() => void>(sendPreview)
  useEffect(() => {
    sendPreviewRef.current = sendPreview
  }, [sendPreview])

  // presentPreview accepts a freshly captured (and cropped, when bounds were
  // available) blob and surfaces it as an overlay so the user can confirm or
  // retake. After PREVIEW_AUTO_SEND_MS the auto-timer proceeds with Send so
  // the fire-and-forget feel is preserved when the kid keeps scanning.
  const presentPreview = useCallback((blob: Blob, manual: boolean) => {
    previewBlobRef.current = blob
    previewWasManualRef.current = manual
    const url = URL.createObjectURL(blob)
    previewUrlRef.current = url
    setPreviewUrl(url)
    scanPhaseRef.current = 'preview'
    setScanPhase('preview')
    previewTimerRef.current = window.setTimeout(() => {
      previewTimerRef.current = null
      sendPreviewRef.current()
    }, PREVIEW_AUTO_SEND_MS)
  }, [])

  const presentPreviewRef = useRef<(blob: Blob, manual: boolean) => void>(presentPreview)
  useEffect(() => {
    presentPreviewRef.current = presentPreview
  }, [presentPreview])

  const handlePreviewSend = useCallback(() => {
    sendPreview()
  }, [sendPreview])

  const handlePreviewRetake = useCallback(() => {
    scanPhaseRef.current = 'idle'
    setScanPhase('idle')
    resumeScanning()
  }, [resumeScanning])

  // Auto-detection rAF loop. Activates once camera permission is granted and
  // runs until the component unmounts or a successful capture freezes the
  // scanner. Each tick downsamples the current frame, runs Sobel edges, and
  // drives the searching → candidate → locked → captured state machine.
  useEffect(() => {
    if (permissionState !== 'granted') return
    if (typeof window === 'undefined' || typeof window.requestAnimationFrame !== 'function') return

    let cancelled = false

    const captureLocked = (sourceCanvas: HTMLCanvasElement) => {
      // Synchronous transition to `captured` guards against double-fire if
      // the rAF callback is invoked again before toBlob resolves.
      scanStatusRef.current = 'captured'
      setScanStatus('captured')
      const video = videoRef.current
      if (video && typeof video.pause === 'function') {
        try {
          video.pause()
        } catch {
          // Pausing can throw on some mobile browsers if the element is in
          // an unexpected state — visually freezing is best-effort.
        }
      }

      const revertToSearching = () => {
        scanStatusRef.current = 'searching'
        setScanStatus('searching')
        candidateBoundsRef.current = null
        candidateTicksRef.current = 0
        lastTickRef.current = 0
        lockedBoundsRef.current = null
        if (video && typeof video.play === 'function') {
          try {
            void video.play()
          } catch {
            // best-effort
          }
        }
        if (!cancelled && tickFnRef.current) {
          rafIdRef.current = window.requestAnimationFrame(tickFnRef.current)
        }
      }

      // Compute the crop rectangle from the locked bounds. detectCardRectangle
      // returns bounds in the same coordinate space as its input ImageData,
      // which is the source canvas at full video resolution — no extra scaling
      // needed. Pad by 5% on each axis to avoid clipping the card border / set
      // symbol / collector number, then clamp to the canvas frame.
      const bounds = lockedBoundsRef.current
      const fullW = sourceCanvas.width
      const fullH = sourceCanvas.height
      let sx = 0
      let sy = 0
      let sw = fullW
      let sh = fullH
      if (bounds && fullW > 0 && fullH > 0) {
        const padX = bounds.w * CAPTURE_CROP_PAD
        const padY = bounds.h * CAPTURE_CROP_PAD
        sx = bounds.x - padX
        sy = bounds.y - padY
        sw = bounds.w + padX * 2
        sh = bounds.h + padY * 2
        if (sx < 0) { sw += sx; sx = 0 }
        if (sy < 0) { sh += sy; sy = 0 }
        if (sx + sw > fullW) sw = fullW - sx
        if (sy + sh > fullH) sh = fullH - sy
        // Defensive: if clamping produced a degenerate or invalid rect, fall
        // back to the uncropped frame so capture still proceeds.
        if (!Number.isFinite(sw) || !Number.isFinite(sh) || sw <= 0 || sh <= 0) {
          sx = 0; sy = 0; sw = fullW; sh = fullH
        }
      }

      const outCanvas = canvasRef.current
      if (!outCanvas) {
        revertToSearching()
        return
      }
      outCanvas.width = Math.max(1, Math.round(sw))
      outCanvas.height = Math.max(1, Math.round(sh))
      const outCtx = outCanvas.getContext('2d')
      if (!outCtx || typeof outCtx.drawImage !== 'function') {
        revertToSearching()
        return
      }
      try {
        outCtx.drawImage(
          sourceCanvas,
          sx, sy, sw, sh,
          0, 0, outCanvas.width, outCanvas.height,
        )
      } catch {
        revertToSearching()
        return
      }

      const dispatchBlob = (blob: Blob | null) => {
        if (blob) {
          presentPreviewRef.current(blob, false)
        } else {
          // toBlob yielded null (or toBlob was unavailable and toDataURL failed).
          // Revert so the scanner is not permanently frozen.
          revertToSearching()
        }
      }

      if (typeof outCanvas.toBlob === 'function') {
        outCanvas.toBlob(dispatchBlob, 'image/jpeg', 0.95)
      } else {
        // toDataURL fallback for environments where toBlob is unavailable.
        try {
          const dataUrl = outCanvas.toDataURL('image/jpeg', 0.95)
          const [header, b64] = dataUrl.split(',')
          const mime = header.match(/:(.*?);/)?.[1] ?? 'image/jpeg'
          const bytes = atob(b64)
          const u8 = new Uint8Array(bytes.length)
          for (let i = 0; i < bytes.length; i++) u8[i] = bytes.charCodeAt(i)
          dispatchBlob(new Blob([u8], { type: mime }))
        } catch {
          dispatchBlob(null)
        }
      }
    }

    const tick = (timestamp: number) => {
      if (cancelled) return
      // Stop the loop entirely once we are submitting or previewing — it
      // will be restarted by resumeScanning when the user moves on. The
      // daily-limit gate also halts the loop so a kid who has hit the cap
      // can't keep auto-firing into 429s.
      if (
        scanPhaseRef.current === 'preview' ||
        scanPhaseRef.current === 'submitting'
      ) {
        rafIdRef.current = null
        return
      }
      if (dailyLimitReachedRef.current) {
        rafIdRef.current = null
        return
      }
      if (scanStatusRef.current === 'captured') return

      if (timestamp - lastTickRef.current < DETECT_TICK_MS) {
        rafIdRef.current = window.requestAnimationFrame(tick)
        return
      }
      lastTickRef.current = timestamp

      try {
        const video = videoRef.current
        if (!video || video.readyState < 2 || video.videoWidth === 0 || video.videoHeight === 0) {
          rafIdRef.current = window.requestAnimationFrame(tick)
          return
        }

        let canvas = detectCanvasRef.current
        if (!canvas) {
          canvas = document.createElement('canvas')
          detectCanvasRef.current = canvas
        }
        const w = video.videoWidth
        const h = video.videoHeight
        if (canvas.width !== w) canvas.width = w
        if (canvas.height !== h) canvas.height = h

        const ctx = canvas.getContext('2d', { willReadFrequently: true })
        if (!ctx || typeof ctx.drawImage !== 'function' || typeof ctx.getImageData !== 'function') {
          rafIdRef.current = window.requestAnimationFrame(tick)
          return
        }

        ctx.drawImage(video, 0, 0, w, h)
        const imageData = ctx.getImageData(0, 0, w, h)
        const detection = detectCardRectangle(imageData)

        if (!detection) {
          if (scanStatusRef.current !== 'searching') {
            scanStatusRef.current = 'searching'
            setScanStatus('searching')
          }
          candidateBoundsRef.current = null
          candidateTicksRef.current = 0
        } else {
          const prev = candidateBoundsRef.current
          if (
            prev &&
            scanStatusRef.current === 'candidate' &&
            isWithinTolerance(prev, detection, CANDIDATE_TOLERANCE, w, h)
          ) {
            candidateTicksRef.current += 1
            candidateBoundsRef.current = detection
            if (candidateTicksRef.current >= TICKS_TO_LOCK) {
              scanStatusRef.current = 'locked'
              setScanStatus('locked')
              // Auto-trigger respects the post-add cooldown — if we are still
              // in the debounce window, drop back to searching instead.
              if (Date.now() < cooldownUntilRef.current) {
                scanStatusRef.current = 'searching'
                setScanStatus('searching')
                candidateBoundsRef.current = null
                candidateTicksRef.current = 0
                rafIdRef.current = window.requestAnimationFrame(tick)
                return
              }
              // Remember the bounds we locked on so captureLocked can crop the
              // JPEG to just the card region.
              lockedBoundsRef.current = detection
              captureLocked(canvas)
              return
            }
          } else {
            candidateBoundsRef.current = detection
            candidateTicksRef.current = 1
            if (scanStatusRef.current !== 'candidate') {
              scanStatusRef.current = 'candidate'
              setScanStatus('candidate')
            }
          }
        }
      } catch {
        // Any unexpected error in the detect path is swallowed — the manual
        // shutter remains available as a fallback.
      }

      if (!cancelled) {
        rafIdRef.current = window.requestAnimationFrame(tick)
      }
    }

    tickFnRef.current = tick
    rafIdRef.current = window.requestAnimationFrame(tick)

    return () => {
      cancelled = true
      tickFnRef.current = null
      if (rafIdRef.current !== null) {
        window.cancelAnimationFrame(rafIdRef.current)
        rafIdRef.current = null
      }
    }
  }, [permissionState])

  const handleClose = useCallback(() => {
    const stream = streamRef.current
    if (stream) {
      stream.getTracks().forEach(track => track.stop())
      streamRef.current = null
    }
    const ctl = scanAbortRef.current
    if (ctl) {
      ctl.abort()
      scanAbortRef.current = null
    }
    if (cooldownTimerRef.current !== null) {
      window.clearTimeout(cooldownTimerRef.current)
      cooldownTimerRef.current = null
    }
    if (queuedToastTimerRef.current !== null) {
      window.clearTimeout(queuedToastTimerRef.current)
      queuedToastTimerRef.current = null
    }
    clearPreview()
    onClose()
  }, [clearPreview, onClose])

  // Lock body scroll while the scanner is mounted, just like Dialog does.
  useEffect(() => {
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = ''
    }
  }, [])

  // Focus the close button on mount so keyboard/screen-reader users can
  // immediately dismiss the overlay without needing to tab to it.
  useEffect(() => {
    closeButtonRef.current?.focus()
  }, [])

  // When the preview overlay appears, move focus to its first action so
  // keyboard/screen-reader users land inside it rather than staying on
  // whichever control was focused before the overlay rendered.
  useEffect(() => {
    if (scanPhase === 'preview') {
      const overlay = dialogRef.current?.querySelector<HTMLElement>('[data-testid="card-scanner-preview"]')
      if (!overlay) return
      const first = overlay.querySelector<HTMLElement>(
        'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
      )
      first?.focus()
    }
  }, [scanPhase])

  // Escape to dismiss + Tab focus trap (mirrors Dialog behaviour).
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        handleClose()
        return
      }
      if (e.key !== 'Tab' || !dialogRef.current) return
      const trapRoot: ParentNode = dialogRef.current
      const focusable = Array.from(
        trapRoot.querySelectorAll<HTMLElement>(
          'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
        ),
      )
      if (focusable.length === 0) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (e.shiftKey) {
        if (document.activeElement === first) {
          e.preventDefault()
          last.focus()
        }
      } else {
        if (document.activeElement === last) {
          e.preventDefault()
          first.focus()
        }
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [handleClose])

  const handleManualCapture = useCallback(() => {
    if (
      scanPhaseRef.current === 'preview' ||
      scanPhaseRef.current === 'submitting'
    ) return
    if (dailyLimitReachedRef.current) return
    // Stop the auto-detect loop before the async toBlob call to prevent a race
    // where the rAF fires an extra auto-capture while this capture is in progress.
    scanStatusRef.current = 'captured'
    setScanStatus('captured')

    const revertToSearching = () => {
      scanStatusRef.current = 'searching'
      setScanStatus('searching')
    }

    const video = videoRef.current
    const canvas = canvasRef.current
    if (!video || !canvas) { revertToSearching(); return }
    const width = video.videoWidth
    const height = video.videoHeight
    if (width === 0 || height === 0) { revertToSearching(); return }
    canvas.width = width
    canvas.height = height
    const ctx = canvas.getContext('2d')
    if (!ctx) { revertToSearching(); return }
    ctx.drawImage(video, 0, 0, width, height)
    canvas.toBlob(
      blob => {
        if (blob) {
          presentPreviewRef.current(blob, true)
        } else {
          revertToSearching()
        }
      },
      'image/jpeg',
      0.92,
    )
  }, [])

  const handleTorchToggle = useCallback(async () => {
    const stream = streamRef.current
    if (!stream) return
    const [track] = stream.getVideoTracks()
    if (!track) return
    const next = !torchOn
    try {
      await track.applyConstraints({
        advanced: [{ torch: next }],
      } as ExtendedMediaTrackConstraints)
      setTorchOn(next)
    } catch {
      // Torch toggling can fail mid-session if the device hot-revokes the
      // capability; surface no error UI — the button simply does nothing.
    }
  }, [torchOn])

  return (
    // Outer layer always covers the full viewport so clicks on the uncovered
    // area (desktop inset) cannot reach the Dialog mounted behind the scanner.
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black md:bg-black/70">
    <div
      ref={dialogRef}
      role="dialog"
      aria-modal="true"
      aria-label={t('scanner.dialogLabel')}
      className="relative bg-black flex items-center justify-center w-full h-full md:h-[80vh] md:max-w-2xl md:rounded-lg md:overflow-hidden md:shadow-2xl"
    >
      {permissionState === 'prompting' && (
        <p className="px-6 text-center text-sm text-gray-200">
          {t('scanner.requesting')}
        </p>
      )}

      {permissionState === 'denied' && (
        <div className="px-6 text-center space-y-4 max-w-sm">
          <p className="text-sm text-gray-200">{t('scanner.permissionDenied')}</p>
          <button
            type="button"
            onClick={handleClose}
            data-testid="card-scanner-manual-entry"
            className="px-4 py-2 rounded bg-emerald-600 hover:bg-emerald-500 text-white text-sm cursor-pointer"
          >
            {t('scanner.enterManually')}
          </button>
        </div>
      )}

      {permissionState === 'unavailable' && (
        <div className="px-6 text-center space-y-4 max-w-sm">
          <p className="text-sm text-gray-200">{t('scanner.cameraUnavailable')}</p>
          <button
            type="button"
            onClick={handleClose}
            data-testid="card-scanner-manual-entry"
            className="px-4 py-2 rounded bg-emerald-600 hover:bg-emerald-500 text-white text-sm cursor-pointer"
          >
            {t('scanner.enterManually')}
          </button>
        </div>
      )}

      {permissionState === 'unsupported' && (
        <div className="px-6 text-center space-y-4 max-w-sm">
          <p className="text-sm text-gray-200">{t('scanner.unsupported')}</p>
          <button
            type="button"
            onClick={handleClose}
            data-testid="card-scanner-manual-entry"
            className="px-4 py-2 rounded bg-emerald-600 hover:bg-emerald-500 text-white text-sm cursor-pointer"
          >
            {t('scanner.enterManually')}
          </button>
        </div>
      )}

      {permissionState === 'granted' && (
        <>
          <video
            ref={videoRef}
            autoPlay
            playsInline
            muted
            data-testid="card-scanner-video"
            className="absolute inset-0 w-full h-full object-cover"
          />
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
            <div
              data-testid="card-scanner-guide"
              className="w-[80%] max-w-sm border-2 border-white/80 rounded-lg shadow-[0_0_0_9999px_rgba(0,0,0,0.45)]"
              style={{ aspectRatio: CARD_GUIDE_ASPECT }}
            />
          </div>

          {(scanStatus === 'candidate' || scanStatus === 'locked') && scanPhase === 'idle' && (
            <div
              data-testid="card-scanner-status"
              aria-live="polite"
              className="pointer-events-none absolute top-20 left-1/2 -translate-x-1/2 px-4 py-2 rounded-full bg-black/70 text-white text-sm font-medium shadow-lg"
            >
              {t('scanner.holdSteady')}
            </div>
          )}

          {queuedToastVisible && !dailyLimitReached && (
            <div
              data-testid="card-scanner-queued-toast"
              role="status"
              aria-live="polite"
              className="absolute top-4 left-1/2 -translate-x-1/2 z-10 flex items-center gap-2 px-4 py-2 rounded-full bg-emerald-600 text-white text-sm font-medium shadow-lg"
            >
              <span>{t('scanner.queuedToast')}</span>
              <Link
                to="/pokemon/scanned"
                onClick={handleClose}
                className="underline hover:no-underline"
                data-testid="card-scanner-queued-toast-link"
              >
                {t('scanner.queuedToastLink')}
              </Link>
            </div>
          )}

          {dailyLimitReached && (
            <div
              data-testid="card-scanner-daily-limit"
              role="alert"
              aria-live="assertive"
              className="absolute top-20 left-1/2 -translate-x-1/2 z-10 px-4 py-2 rounded-lg bg-red-600 text-white text-sm font-medium shadow-lg max-w-[80%] text-center"
            >
              {t('scanner.dailyLimit')}
            </div>
          )}

          {scanPhase === 'submitting' && (
            <div
              data-testid="card-scanner-spinner"
              role="status"
              aria-live="polite"
              className="absolute inset-0 z-10 flex flex-col items-center justify-center gap-3 bg-black/70 text-white"
            >
              <Loader2 size={32} className="animate-spin" aria-hidden="true" />
              <p className="text-sm">{t('scanner.scanning')}</p>
            </div>
          )}

          <button
            type="button"
            onClick={handleManualCapture}
            disabled={
              scanPhase === 'preview' || scanPhase === 'submitting' || dailyLimitReached
            }
            aria-label={t('scanner.shutter')}
            data-testid="card-scanner-shutter"
            className="absolute bottom-8 left-1/2 -translate-x-1/2 flex items-center justify-center h-16 w-16 rounded-full bg-white text-gray-900 shadow-lg ring-4 ring-white/30 hover:bg-gray-100 disabled:opacity-60 disabled:cursor-not-allowed cursor-pointer"
          >
            <Camera size={28} />
          </button>

          {scanPhase === 'preview' && previewUrl && (
            <div
              data-testid="card-scanner-preview"
              role="dialog"
              aria-label={t('scanner.preview.title')}
              className="absolute inset-0 z-10 flex flex-col items-center justify-center gap-4 bg-black/85 p-4"
            >
              <img
                src={previewUrl}
                alt={t('scanner.preview.imageAlt')}
                data-testid="card-scanner-preview-image"
                className="max-h-[60vh] max-w-full rounded-lg shadow-xl object-contain"
              />
              <div className="flex gap-3">
                <button
                  type="button"
                  data-testid="card-scanner-preview-retake"
                  onClick={handlePreviewRetake}
                  className="px-5 py-2 rounded bg-gray-700 hover:bg-gray-600 text-white text-sm cursor-pointer"
                >
                  {t('scanner.preview.retake')}
                </button>
                <button
                  type="button"
                  data-testid="card-scanner-preview-send"
                  onClick={handlePreviewSend}
                  className="px-5 py-2 rounded bg-emerald-600 hover:bg-emerald-500 text-white text-sm cursor-pointer"
                >
                  {t('scanner.preview.send')}
                </button>
              </div>
            </div>
          )}

          {torchSupported && (
            <button
              type="button"
              onClick={() => { void handleTorchToggle() }}
              aria-label={torchOn ? t('scanner.torchOff') : t('scanner.torchOn')}
              aria-pressed={torchOn}
              data-testid="card-scanner-torch"
              className="absolute top-4 left-4 flex items-center justify-center h-10 w-10 rounded-full bg-black/60 hover:bg-black/80 text-white cursor-pointer"
            >
              {torchOn ? <Flashlight size={20} /> : <FlashlightOff size={20} />}
            </button>
          )}

        </>
      )}

      <button
        ref={closeButtonRef}
        type="button"
        onClick={handleClose}
        aria-label={t('scanner.close')}
        data-testid="card-scanner-close"
        className="absolute top-4 right-4 z-20 flex items-center justify-center h-10 w-10 rounded-full bg-black/60 hover:bg-black/80 text-white cursor-pointer"
      >
        <X size={20} />
      </button>

      <canvas ref={canvasRef} className="hidden" aria-hidden="true" />
    </div>
    <ToastList toasts={toasts} />
    </div>
  )
}
