import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Camera, Flashlight, FlashlightOff, X } from 'lucide-react'
import {
  detectCardRectangle,
  isWithinTolerance,
  type DetectedRectangle,
  type RectangleDetectorStatus,
} from './rectangleDetector'

export interface CardScannerProps {
  onCapture: (blob: Blob) => void
  onClose: () => void
}

type PermissionState = 'prompting' | 'granted' | 'denied' | 'unavailable' | 'unsupported'

// Throttle the auto-detect tick to ~2/sec. Two consecutive matching ticks
// (~1s of stable framing) promote a candidate to `locked`.
const DETECT_TICK_MS = 500
const TICKS_TO_LOCK = 2
// Allow up to ±5% drift between consecutive candidate detections.
const CANDIDATE_TOLERANCE = 0.05

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

export default function CardScanner({ onCapture, onClose }: CardScannerProps) {
  const { t } = useTranslation('pokemon')

  const [permissionState, setPermissionState] = useState<PermissionState>(() =>
    typeof navigator.mediaDevices?.getUserMedia === 'function' ? 'prompting' : 'unsupported',
  )
  const [torchOn, setTorchOn] = useState(false)
  const [torchSupported, setTorchSupported] = useState(false)
  const [scanStatus, setScanStatus] = useState<RectangleDetectorStatus>('searching')

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
  const onCaptureRef = useRef(onCapture)
  useEffect(() => {
    onCaptureRef.current = onCapture
  }, [onCapture])

  // Acquire the camera on mount; stop tracks on unmount so the camera LED
  // turns off even if the parent unmounts the scanner without calling onClose.
  useEffect(() => {
    let cancelled = false

    if (!navigator.mediaDevices?.getUserMedia) return

    void (async () => {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({
          video: { facingMode: { ideal: 'environment' } },
        })
        if (cancelled) {
          stream.getTracks().forEach(track => track.stop())
          return
        }
        streamRef.current = stream
        if (videoRef.current) {
          videoRef.current.srcObject = stream
        }
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
    }
  }, [])

  // Auto-detection rAF loop. Activates once camera permission is granted and
  // runs until the component unmounts or a successful capture freezes the
  // scanner. Each tick downsamples the current frame, runs Sobel edges, and
  // drives the searching → candidate → locked → captured state machine.
  useEffect(() => {
    if (permissionState !== 'granted') return
    if (typeof window === 'undefined' || typeof window.requestAnimationFrame !== 'function') return

    let cancelled = false

    const captureLocked = (canvas: HTMLCanvasElement) => {
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

      const dispatchBlob = (blob: Blob | null) => {
        if (blob) {
          onCaptureRef.current(blob)
        } else {
          // toBlob yielded null (or toBlob was unavailable and toDataURL failed).
          // Revert so the scanner is not permanently frozen.
          scanStatusRef.current = 'searching'
          setScanStatus('searching')
          candidateBoundsRef.current = null
          candidateTicksRef.current = 0
          lastTickRef.current = 0
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
      }

      if (typeof canvas.toBlob === 'function') {
        canvas.toBlob(dispatchBlob, 'image/jpeg', 0.85)
      } else {
        // toDataURL fallback for environments where toBlob is unavailable.
        try {
          const dataUrl = canvas.toDataURL('image/jpeg', 0.85)
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
    onClose()
  }, [onClose])

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

  // Escape to dismiss + Tab focus trap (mirrors Dialog behaviour).
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        handleClose()
        return
      }
      if (e.key !== 'Tab' || !dialogRef.current) return
      const focusable = Array.from(
        dialogRef.current.querySelectorAll<HTMLElement>(
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

  const handleCapture = useCallback(() => {
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
          onCapture(blob)
        } else {
          revertToSearching()
        }
      },
      'image/jpeg',
      0.92,
    )
  }, [onCapture])

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

          {(scanStatus === 'candidate' || scanStatus === 'locked') && (
            <div
              data-testid="card-scanner-status"
              aria-live="polite"
              className="pointer-events-none absolute top-20 left-1/2 -translate-x-1/2 px-4 py-2 rounded-full bg-black/70 text-white text-sm font-medium shadow-lg"
            >
              {t('scanner.holdSteady')}
            </div>
          )}

          <button
            type="button"
            onClick={handleCapture}
            aria-label={t('scanner.shutter')}
            data-testid="card-scanner-shutter"
            className="absolute bottom-8 left-1/2 -translate-x-1/2 flex items-center justify-center h-16 w-16 rounded-full bg-white text-gray-900 shadow-lg ring-4 ring-white/30 hover:bg-gray-100 cursor-pointer"
          >
            <Camera size={28} />
          </button>

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
        className="absolute top-4 right-4 flex items-center justify-center h-10 w-10 rounded-full bg-black/60 hover:bg-black/80 text-white cursor-pointer"
      >
        <X size={20} />
      </button>

      <canvas ref={canvasRef} className="hidden" aria-hidden="true" />
    </div>
    </div>
  )
}
