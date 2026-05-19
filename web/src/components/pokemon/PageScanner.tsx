import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Flashlight, FlashlightOff, LayoutGrid, Loader2, X } from 'lucide-react'
import { detectGrid, cropCellsToCanvases } from './rectangleDetector'
import { usePokemonCamera } from './usePokemonCamera'
import ToastList from '../ToastList'
import { useToast } from '../../hooks/useToast'

export interface PageScannerProps {
  onClose: () => void
  // rows × cols of the binder layout we expect to crop out of the captured
  // page. Defaults to a 3×3 framing guide so the existing 9-pocket binder
  // pages line up with the overlay; a 4×3 layout is supported as well.
  rows?: number
  cols?: number
}

// SCAN_TIMEOUT_MS bounds the page-upload POST. The endpoint persists N child
// jobs synchronously so it is heavier than the single-card queue; 60 s gives
// it room for the larger payload while still surfacing a timeout if the
// network or server stalls.
const SCAN_TIMEOUT_MS = 60000

// Default binder layout. 3×3 lines up with the standard 9-pocket page that
// the rectangle detector was tuned against. Callers can override via the
// rows/cols props if they need a 4×3 page instead.
const DEFAULT_ROWS = 3
const DEFAULT_COLS = 3

// PageScanner reuses the camera lifecycle from usePokemonCamera at 4K and
// renders a framing-guide overlay (rows × cols) over the live preview. The
// user lines up a binder page with the overlay and presses the manual
// shutter; on capture we grab a frame, run detectGrid + cropCellsToCanvases,
// POST the N crops + cells JSON to /api/pokemon/scans/page, then navigate
// to /pokemon/scanned?page=<id>. There is no auto-capture loop and no
// per-cell rectangle detection on the live preview — the overlay is the
// only guidance and the shutter is the only trigger.
export default function PageScanner({
  onClose,
  rows = DEFAULT_ROWS,
  cols = DEFAULT_COLS,
}: PageScannerProps) {
  const { t } = useTranslation('pokemon')
  const navigate = useNavigate()
  const { toasts, showToast } = useToast()

  const {
    videoRef,
    permissionState,
    torchSupported,
    torchOn,
    toggleTorch,
    stopCamera,
  } = usePokemonCamera({ width: 3840, height: 2160 })

  const [submitting, setSubmitting] = useState(false)
  const [dailyLimitReached, setDailyLimitReached] = useState(false)

  const canvasRef = useRef<HTMLCanvasElement>(null)
  const closeButtonRef = useRef<HTMLButtonElement>(null)
  const dialogRef = useRef<HTMLDivElement>(null)
  const submittingRef = useRef(false)
  const dailyLimitRef = useRef(false)
  // Track the in-flight POST so unmount / close can abort it cleanly without
  // leaving a hanging fetch that finally-fires setState on a torn-down tree.
  const abortRef = useRef<AbortController | null>(null)

  // Cleanup on unmount: abort the in-flight POST. The camera teardown is
  // handled by usePokemonCamera itself.
  useEffect(() => {
    return () => {
      const ctl = abortRef.current
      if (ctl) {
        ctl.abort()
        abortRef.current = null
      }
    }
  }, [])

  const handleClose = useCallback(() => {
    stopCamera()
    const ctl = abortRef.current
    if (ctl) {
      ctl.abort()
      abortRef.current = null
    }
    onClose()
  }, [onClose, stopCamera])

  // Lock body scroll while the scanner is mounted, mirroring Dialog and
  // CardScanner so the page underneath doesn't scroll on mobile when the
  // user pinches/swipes against the framing-guide overlay.
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

  // Escape to dismiss + Tab focus trap (mirrors CardScanner / Dialog).
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

  // canvasToJpegBlob is the toBlob / toDataURL fallback we share between the
  // source-frame grab and each per-cell crop. happy-dom / test environments
  // often lack toBlob, so the fallback path matters for the unit tests too.
  const canvasToJpegBlob = useCallback((canvas: HTMLCanvasElement): Promise<Blob | null> => {
    return new Promise(resolve => {
      if (typeof canvas.toBlob === 'function') {
        canvas.toBlob(blob => resolve(blob), 'image/jpeg', 0.92)
        return
      }
      try {
        const dataUrl = canvas.toDataURL('image/jpeg', 0.92)
        const [header, b64] = dataUrl.split(',')
        const mime = header.match(/:(.*?);/)?.[1] ?? 'image/jpeg'
        const bytes = atob(b64)
        const u8 = new Uint8Array(bytes.length)
        for (let i = 0; i < bytes.length; i++) u8[i] = bytes.charCodeAt(i)
        resolve(new Blob([u8], { type: mime }))
      } catch {
        resolve(null)
      }
    })
  }, [])

  const handleShutter = useCallback(async () => {
    if (submittingRef.current || dailyLimitRef.current) return

    const video = videoRef.current
    const canvas = canvasRef.current
    if (!video || !canvas) return
    const vw = video.videoWidth
    const vh = video.videoHeight
    if (vw === 0 || vh === 0) {
      showToast(t('pageScanner.errors.noFrame'), 'error')
      return
    }

    submittingRef.current = true
    setSubmitting(true)

    try {
      canvas.width = vw
      canvas.height = vh
      const ctx = canvas.getContext('2d', { willReadFrequently: true })
      if (!ctx) {
        showToast(t('pageScanner.errors.captureFailed'), 'error')
        return
      }
      ctx.drawImage(video, 0, 0, vw, vh)

      let imageData: ImageData
      try {
        imageData = ctx.getImageData(0, 0, vw, vh)
      } catch {
        showToast(t('pageScanner.errors.captureFailed'), 'error')
        return
      }

      const cells = detectGrid(imageData, { rows, cols })
      if (cells.length === 0) {
        showToast(t('pageScanner.errors.noGrid'), 'error')
        return
      }

      const cropCanvases = cropCellsToCanvases(canvas, cells)
      if (cropCanvases.length === 0) {
        showToast(t('pageScanner.errors.captureFailed'), 'error')
        return
      }

      const cropBlobs: Blob[] = []
      for (const c of cropCanvases) {
        const blob = await canvasToJpegBlob(c)
        if (!blob) {
          showToast(t('pageScanner.errors.captureFailed'), 'error')
          return
        }
        cropBlobs.push(blob)
      }

      const formData = new FormData()
      cropBlobs.forEach((blob, i) => {
        formData.append('images', blob, `card-${i}.jpg`)
      })
      const cellsJson = JSON.stringify(
        cells.map(c => ({ row: c.row, col: c.col })),
      )
      formData.append('cells', cellsJson)

      const controller = new AbortController()
      abortRef.current = controller
      let timedOut = false
      const timer = window.setTimeout(() => {
        timedOut = true
        controller.abort()
      }, SCAN_TIMEOUT_MS)

      try {
        const res = await fetch('/api/pokemon/scans/page', {
          method: 'POST',
          credentials: 'include',
          body: formData,
          signal: controller.signal,
        })
        if (res.status === 429) {
          dailyLimitRef.current = true
          setDailyLimitReached(true)
          return
        }
        if (!res.ok) {
          showToast(t('pageScanner.errors.uploadFailed'), 'error')
          return
        }
        const data: { page_id?: number } = await res.json().catch(() => ({}))
        const pageId = data.page_id
        if (typeof pageId !== 'number') {
          showToast(t('pageScanner.errors.uploadFailed'), 'error')
          return
        }
        stopCamera()
        navigate(`/pokemon/scanned?page=${pageId}`)
        onClose()
      } catch (err) {
        if (controller.signal.aborted) {
          if (timedOut) {
            showToast(t('pageScanner.errors.timedOut'), 'error')
          }
          return
        }
        showToast(
          err instanceof Error ? err.message : t('pageScanner.errors.uploadFailed'),
          'error',
        )
      } finally {
        window.clearTimeout(timer)
        if (abortRef.current === controller) abortRef.current = null
      }
    } finally {
      submittingRef.current = false
      setSubmitting(false)
    }
  }, [
    canvasToJpegBlob,
    cols,
    navigate,
    onClose,
    rows,
    showToast,
    stopCamera,
    t,
    videoRef,
  ])

  // gridLines is a stable list of fractional positions used to draw the
  // overlay's column / row dividers. Excludes 0 and 1 (the outer border
  // is rendered by the wrapping element).
  const colLines = Array.from({ length: cols - 1 }, (_, i) => ((i + 1) / cols) * 100)
  const rowLines = Array.from({ length: rows - 1 }, (_, i) => ((i + 1) / rows) * 100)

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black md:bg-black/70">
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-label={t('pageScanner.dialogLabel')}
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
              data-testid="page-scanner-manual-entry"
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
              data-testid="page-scanner-manual-entry"
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
              data-testid="page-scanner-manual-entry"
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
              data-testid="page-scanner-video"
              className="absolute inset-0 w-full h-full object-cover"
            />

            <div
              data-testid="page-scanner-grid-guide"
              role="presentation"
              aria-hidden="true"
              className="pointer-events-none absolute inset-6 sm:inset-10 border-2 border-white/80 rounded-lg shadow-[0_0_0_9999px_rgba(0,0,0,0.45)]"
            >
              {colLines.map(pct => (
                <div
                  key={`col-${pct}`}
                  data-testid="page-scanner-grid-col-line"
                  className="absolute top-0 bottom-0 w-px bg-white/70"
                  style={{ left: `${pct}%` }}
                />
              ))}
              {rowLines.map(pct => (
                <div
                  key={`row-${pct}`}
                  data-testid="page-scanner-grid-row-line"
                  className="absolute left-0 right-0 h-px bg-white/70"
                  style={{ top: `${pct}%` }}
                />
              ))}
            </div>

            {dailyLimitReached && (
              <div
                data-testid="page-scanner-daily-limit"
                role="alert"
                aria-live="assertive"
                className="absolute top-20 left-1/2 -translate-x-1/2 z-10 px-4 py-2 rounded-lg bg-red-600 text-white text-sm font-medium shadow-lg max-w-[80%] text-center"
              >
                {t('scanner.dailyLimit')}
              </div>
            )}

            {submitting && (
              <div
                data-testid="page-scanner-spinner"
                role="status"
                aria-live="polite"
                className="absolute inset-0 z-10 flex flex-col items-center justify-center gap-3 bg-black/70 text-white"
              >
                <Loader2 size={32} className="animate-spin" aria-hidden="true" />
                <p className="text-sm">{t('pageScanner.uploading')}</p>
              </div>
            )}

            <button
              type="button"
              onClick={() => { void handleShutter() }}
              disabled={submitting || dailyLimitReached}
              aria-label={t('pageScanner.shutter')}
              data-testid="page-scanner-shutter"
              className="absolute bottom-8 left-1/2 -translate-x-1/2 flex items-center justify-center h-16 w-16 rounded-full bg-white text-gray-900 shadow-lg ring-4 ring-white/30 hover:bg-gray-100 disabled:opacity-60 disabled:cursor-not-allowed cursor-pointer"
            >
              <LayoutGrid size={28} />
            </button>

            {torchSupported && (
              <button
                type="button"
                onClick={() => { void toggleTorch() }}
                aria-label={torchOn ? t('scanner.torchOff') : t('scanner.torchOn')}
                aria-pressed={torchOn}
                data-testid="page-scanner-torch"
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
          data-testid="page-scanner-close"
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
