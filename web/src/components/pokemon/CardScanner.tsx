import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Camera, Flashlight, FlashlightOff, X } from 'lucide-react'

export interface CardScannerProps {
  onCapture: (blob: Blob) => void
  onClose: () => void
}

type PermissionState = 'prompting' | 'granted' | 'denied' | 'unsupported'

// Pokémon TCG cards are 63x88mm — aspect ratio ≈ 0.716. The guide overlay
// uses 5/7 (≈0.714) which is close enough and renders crisply on all viewports.
const CARD_GUIDE_ASPECT = '5 / 7'

interface ExtendedMediaTrackCapabilities extends MediaTrackCapabilities {
  torch?: boolean
}

interface TorchConstraint {
  torch: boolean
}

interface ExtendedMediaTrackConstraintSet extends MediaTrackConstraintSet {
  advanced?: TorchConstraint[]
}

export default function CardScanner({ onCapture, onClose }: CardScannerProps) {
  const { t } = useTranslation('pokemon')

  const [permissionState, setPermissionState] = useState<PermissionState>(() =>
    navigator.mediaDevices?.getUserMedia ? 'prompting' : 'unsupported',
  )
  const [torchOn, setTorchOn] = useState(false)
  const [torchSupported, setTorchSupported] = useState(false)

  const videoRef = useRef<HTMLVideoElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const streamRef = useRef<MediaStream | null>(null)

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
      } catch {
        if (!cancelled) setPermissionState('denied')
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

  const handleClose = useCallback(() => {
    const stream = streamRef.current
    if (stream) {
      stream.getTracks().forEach(track => track.stop())
      streamRef.current = null
    }
    onClose()
  }, [onClose])

  const handleCapture = useCallback(() => {
    const video = videoRef.current
    const canvas = canvasRef.current
    if (!video || !canvas) return
    const width = video.videoWidth
    const height = video.videoHeight
    if (width === 0 || height === 0) return
    canvas.width = width
    canvas.height = height
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    ctx.drawImage(video, 0, 0, width, height)
    canvas.toBlob(
      blob => {
        if (blob) onCapture(blob)
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
      } as ExtendedMediaTrackConstraintSet)
      setTorchOn(next)
    } catch {
      // Torch toggling can fail mid-session if the device hot-revokes the
      // capability; surface no error UI — the button simply does nothing.
    }
  }, [torchOn])

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={t('scanner.dialogLabel')}
      className="fixed inset-0 z-50 bg-black flex items-center justify-center md:inset-8 md:rounded-lg md:overflow-hidden md:max-w-2xl md:mx-auto md:my-auto md:h-[80vh] md:shadow-2xl"
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
  )
}
