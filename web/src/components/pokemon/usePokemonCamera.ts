import type { RefObject } from 'react'
import { useCallback, useEffect, useRef, useState } from 'react'

// PermissionState mirrors the four UX-distinct outcomes of the getUserMedia
// permission prompt that the scanner UIs render fallback copy for:
//   - prompting: request in flight, no decision yet
//   - granted: stream attached, video element will render
//   - denied: user (or prior policy) refused camera access
//   - unavailable: getUserMedia threw for some other reason (NotFoundError,
//     OverconstrainedError, the camera is held by another app, etc.)
//   - unsupported: the browser does not expose navigator.mediaDevices.getUserMedia
export type CameraPermissionState =
  | 'prompting'
  | 'granted'
  | 'denied'
  | 'unavailable'
  | 'unsupported'

interface ExtendedMediaTrackCapabilities extends MediaTrackCapabilities {
  torch?: boolean
}

interface TorchConstraint extends MediaTrackConstraintSet {
  torch: boolean
}

interface ExtendedMediaTrackConstraints extends MediaTrackConstraints {
  advanced?: TorchConstraint[]
}

export interface UsePokemonCameraOptions {
  width: number
  height: number
  facingMode?: string
}

export interface UsePokemonCameraResult {
  videoRef: RefObject<HTMLVideoElement | null>
  streamRef: RefObject<MediaStream | null>
  permissionState: CameraPermissionState
  torchSupported: boolean
  torchOn: boolean
  toggleTorch: () => Promise<void>
  stopCamera: () => void
}

// usePokemonCamera centralises the camera lifecycle shared by CardScanner
// (single-card auto-detect) and PageScanner (binder-page manual shutter):
// it requests getUserMedia with caller-supplied ideal width/height, attaches
// the stream to the rendered <video> element once permission is granted,
// detects torch capability, exposes a toggle that calls applyConstraints,
// and stops all tracks on unmount or via stopCamera so the camera LED turns
// off even if the parent forgets to call its own teardown path.
export function usePokemonCamera(options: UsePokemonCameraOptions): UsePokemonCameraResult {
  const { width, height, facingMode = 'environment' } = options

  const [permissionState, setPermissionState] = useState<CameraPermissionState>(() =>
    typeof navigator.mediaDevices?.getUserMedia === 'function' ? 'prompting' : 'unsupported',
  )
  const [torchSupported, setTorchSupported] = useState(false)
  const [torchOn, setTorchOn] = useState(false)

  const videoRef = useRef<HTMLVideoElement>(null)
  const streamRef = useRef<MediaStream | null>(null)

  // stopCamera tears down the active MediaStream so the camera LED turns off
  // promptly. Safe to call multiple times — second + later calls are no-ops.
  const stopCamera = useCallback(() => {
    const stream = streamRef.current
    if (stream) {
      stream.getTracks().forEach(track => track.stop())
      streamRef.current = null
    }
  }, [])

  // Acquire the camera on mount. The constraints follow the same shape used
  // by the original CardScanner — facingMode + width/height ideals so the
  // browser falls back to the closest available preset automatically.
  useEffect(() => {
    let cancelled = false

    if (!navigator.mediaDevices?.getUserMedia) return

    void (async () => {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({
          video: {
            facingMode: { ideal: facingMode },
            width: { ideal: width },
            height: { ideal: height },
          },
        })
        if (cancelled) {
          stream.getTracks().forEach(track => track.stop())
          return
        }
        streamRef.current = stream
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
      stopCamera()
    }
  }, [width, height, facingMode, stopCamera])

  // Attach the captured stream to the <video> element once permission flips
  // to 'granted' and the element actually exists in the DOM. Without this,
  // srcObject would be assigned before the conditional <video> renders, and
  // the camera preview would stay black even though the stream is live.
  // iOS Safari in particular needs this; it won't reattach implicitly.
  useEffect(() => {
    if (permissionState !== 'granted') return
    const video = videoRef.current
    const stream = streamRef.current
    if (!video || !stream) return
    if (video.srcObject !== stream) {
      video.srcObject = stream
    }
    if (typeof video.play === 'function') {
      void video.play().catch(() => {})
    }
  }, [permissionState])

  const toggleTorch = useCallback(async () => {
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

  return {
    videoRef,
    streamRef,
    permissionState,
    torchSupported,
    torchOn,
    toggleTorch,
    stopCamera,
  }
}
