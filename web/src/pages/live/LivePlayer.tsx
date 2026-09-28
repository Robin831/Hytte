import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Battery, BatteryCharging, BatteryLow, Eye, Maximize, Volume2, VolumeX } from 'lucide-react'
import type Hls from 'hls.js'
import { HttpError, fetchIceServers, getSessionDetail, hangUp, negotiate, type SessionDetail, type ViewerEndpoints } from './liveApi'

type Status = 'loading' | 'waiting' | 'connecting' | 'playing' | 'ended' | 'offline' | 'notFound' | 'error'

const POLL_MS = 5_000
// Broadcaster not on air yet (MediaMTX answers WHEP with 404): retry soon.
const WAITING_RETRY_MS = 3_000
// If WebRTC has not connected in this long, the viewer's network is probably
// blocking it — fall back to HLS.
const CONNECT_TIMEOUT_MS = 12_000
// Consecutive WebRTC failures (connection 'failed' or a non-404 signalling
// error) before switching to HLS.
const MAX_WEBRTC_FAILURES = 2
// Video time not advancing for this long while "playing" means the
// broadcaster's uplink stalled.
const STALL_MS = 4_000

interface LivePlayerProps {
  endpoints: ViewerEndpoints
  /** Called with every poll result (title, stats, ended, replay id…). */
  onDetail?: (detail: SessionDetail) => void
}

export default function LivePlayer({ endpoints, onDetail }: LivePlayerProps) {
  const { t } = useTranslation('livestream')

  const containerRef = useRef<HTMLDivElement>(null)
  const videoRef = useRef<HTMLVideoElement>(null)
  const pcRef = useRef<RTCPeerConnection | null>(null)
  const resourceRef = useRef<string | null>(null)
  const hlsRef = useRef<Hls | null>(null)
  const retryTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const connectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const activeRef = useRef(true)
  const failuresRef = useRef(0)
  const startRef = useRef<() => void>(() => {})
  const onDetailRef = useRef(onDetail)
  // Which session is playing. A personal share link moves on to the owner's
  // next broadcast, so this can change without the page reloading.
  const currentIdRef = useRef<number | null>(null)

  const [detail, setDetail] = useState<SessionDetail | null>(null)
  const [status, setStatus] = useState<Status>('loading')
  const [mode, setMode] = useState<'webrtc' | 'hls'>('webrtc')
  const [muted, setMuted] = useState(true)
  const [stalled, setStalled] = useState(false)
  const [hadPlayed, setHadPlayed] = useState(false)
  // Last good frame, shown behind "back in a moment" instead of black.
  const [lastFrame, setLastFrame] = useState<string | null>(null)

  useEffect(() => {
    onDetailRef.current = onDetail
  }, [onDetail])

  const captureFrame = useCallback(() => {
    const video = videoRef.current
    if (!video || video.videoWidth === 0) return
    try {
      const canvas = document.createElement('canvas')
      canvas.width = video.videoWidth
      canvas.height = video.videoHeight
      canvas.getContext('2d')?.drawImage(video, 0, 0)
      setLastFrame(canvas.toDataURL('image/jpeg', 0.7))
    } catch {
      // Tainted canvas or no 2D context — just show the plain overlay.
    }
  }, [])

  const stopPlayback = useCallback(() => {
    captureFrame()
    if (retryTimerRef.current) clearTimeout(retryTimerRef.current)
    if (connectTimerRef.current) clearTimeout(connectTimerRef.current)
    retryTimerRef.current = null
    connectTimerRef.current = null
    const pc = pcRef.current
    pcRef.current = null
    hangUp(resourceRef.current)
    resourceRef.current = null
    pc?.close()
    hlsRef.current?.destroy()
    hlsRef.current = null
    const video = videoRef.current
    if (video) {
      video.srcObject = null
      video.removeAttribute('src')
    }
  }, [captureFrame])

  const retryLater = useCallback((ms: number) => {
    if (!activeRef.current) return
    if (retryTimerRef.current) clearTimeout(retryTimerRef.current)
    retryTimerRef.current = setTimeout(() => {
      retryTimerRef.current = null
      startRef.current()
    }, ms)
  }, [])

  const startHLS = useCallback(async () => {
    const video = videoRef.current
    if (!video || !activeRef.current) return
    stopPlayback()
    setMode('hls')
    setStatus('connecting')
    // Prefer hls.js wherever MSE exists: recent Chrome also claims native HLS
    // support via canPlayType, but its built-in player cannot parse
    // MediaMTX's low-latency fMP4 + Opus stream. Native HLS is the fallback
    // for browsers without MSE (older iOS Safari).
    const { default: HlsLib } = await import('hls.js')
    if (!activeRef.current) return
    if (HlsLib.isSupported()) {
      const hls = new HlsLib({ lowLatencyMode: true, xhrSetup: xhr => { xhr.withCredentials = true } })
      hlsRef.current = hls
      hls.on(HlsLib.Events.ERROR, (_evt, data) => {
        if (data.fatal) {
          hls.destroy()
          if (hlsRef.current === hls) hlsRef.current = null
          setStatus('waiting')
          retryLater(WAITING_RETRY_MS)
        }
      })
      hls.loadSource(endpoints.hls)
      hls.attachMedia(video)
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = endpoints.hls
    } else {
      setStatus('error')
      return
    }
    video.play().catch(() => {})
  }, [endpoints.hls, stopPlayback, retryLater])

  const startWebRTC = useCallback(async () => {
    const video = videoRef.current
    if (!video || !activeRef.current) return
    stopPlayback()
    setStatus(s => (s === 'waiting' ? 'waiting' : 'connecting'))
    const iceServers = await fetchIceServers(endpoints.ice)
    if (!activeRef.current) return

    let pc: RTCPeerConnection
    try {
      pc = new RTCPeerConnection({ iceServers })
      pc.addTransceiver('video', { direction: 'recvonly' })
      pc.addTransceiver('audio', { direction: 'recvonly' })
    } catch {
      // No usable WebRTC in this browser (or it is disabled): HLS only.
      startHLS()
      return
    }
    pcRef.current = pc
    const stream = new MediaStream()
    pc.addEventListener('track', e => {
      stream.addTrack(e.track)
      if (video.srcObject !== stream) video.srcObject = stream
      video.play().catch(() => {})
    })
    pc.addEventListener('connectionstatechange', () => {
      if (pcRef.current !== pc) return
      if (pc.connectionState === 'connected') {
        if (connectTimerRef.current) clearTimeout(connectTimerRef.current)
        connectTimerRef.current = null
        failuresRef.current = 0
        setStatus('playing')
      } else if (pc.connectionState === 'failed' || pc.connectionState === 'disconnected') {
        // Usually the broadcaster reconnecting (MediaMTX drops readers when
        // the publisher goes away). Retry; after repeated failures assume
        // WebRTC is blocked on our side and use HLS.
        if (pc.connectionState === 'failed') failuresRef.current += 1
        if (failuresRef.current >= MAX_WEBRTC_FAILURES) {
          startHLS()
          return
        }
        setStatus('waiting')
        retryLater(WAITING_RETRY_MS)
      }
    })

    try {
      resourceRef.current = await negotiate(pc, endpoints.whep)
    } catch (err) {
      if (pcRef.current !== pc) return
      pc.close()
      pcRef.current = null
      if (err instanceof HttpError && err.status === 410) {
        setStatus('ended')
        return
      }
      if (err instanceof HttpError && err.status === 404) {
        setStatus('waiting')
        retryLater(WAITING_RETRY_MS)
        return
      }
      failuresRef.current += 1
      if (failuresRef.current >= MAX_WEBRTC_FAILURES) {
        startHLS()
      } else {
        retryLater(WAITING_RETRY_MS)
      }
      return
    }
    connectTimerRef.current = setTimeout(() => {
      connectTimerRef.current = null
      if (pcRef.current === pc && pc.connectionState !== 'connected') startHLS()
    }, CONNECT_TIMEOUT_MS)
  }, [endpoints.ice, endpoints.whep, stopPlayback, retryLater, startHLS])

  useEffect(() => {
    startRef.current = mode === 'hls' ? startHLS : startWebRTC
  }, [mode, startHLS, startWebRTC])

  // Initial load + periodic poll for status/viewers/battery/ended.
  useEffect(() => {
    activeRef.current = true
    currentIdRef.current = null
    const poll = async () => {
      try {
        const d = await getSessionDetail(endpoints.session)
        if (!activeRef.current) return
        setDetail(d)
        onDetailRef.current?.(d)
        if (!d.session) {
          // Personal link, owner offline: wait here and start by itself.
          if (currentIdRef.current !== null) stopPlayback()
          currentIdRef.current = null
          setStatus('offline')
          return
        }
        if (d.session.status === 'ended') {
          stopPlayback()
          setStatus('ended')
          return
        }
        if (currentIdRef.current !== d.session.id) {
          if (currentIdRef.current !== null) stopPlayback()
          currentIdRef.current = d.session.id
          failuresRef.current = 0
          setMode('webrtc')
          setStatus('connecting')
          startWebRTC()
        }
      } catch (err) {
        if (!activeRef.current) return
        if (err instanceof HttpError && err.status === 404) {
          stopPlayback()
          setStatus('notFound')
        }
      }
    }
    poll()
    const pollId = setInterval(poll, POLL_MS)
    return () => {
      activeRef.current = false
      clearInterval(pollId)
      stopPlayback()
    }
  }, [endpoints.session, startWebRTC, stopPlayback])

  // Stall detection: the connection can stay up while no frames arrive
  // (broadcaster's uplink frozen). Show "back in a moment" over the frame.
  // Count decoded frames where the browser reports them: for a WebRTC
  // MediaStream, currentTime keeps ticking with the wall clock even when no
  // video arrives.
  useEffect(() => {
    if (status !== 'playing') return
    let lastProgress = -1
    let lastChange = performance.now()
    const id = setInterval(() => {
      const v = videoRef.current
      if (!v) return
      const progress = v.getVideoPlaybackQuality?.().totalVideoFrames ?? v.currentTime
      if (progress !== lastProgress) {
        lastProgress = progress
        lastChange = performance.now()
        setStalled(false)
      } else if (performance.now() - lastChange > STALL_MS) {
        setStalled(true)
      }
    }, 1000)
    return () => {
      clearInterval(id)
      setStalled(false)
    }
  }, [status])

  const toggleMute = () => {
    const next = !muted
    setMuted(next)
    const video = videoRef.current
    if (video) {
      video.muted = next
      if (!next) video.play().catch(() => {})
    }
  }

  const goFullscreen = () => {
    const el = containerRef.current
    const video = videoRef.current as (HTMLVideoElement & { webkitEnterFullscreen?: () => void }) | null
    if (el?.requestFullscreen) {
      el.requestFullscreen().catch(() => video?.webkitEnterFullscreen?.())
    } else {
      // iOS Safari only supports fullscreen on the video element itself.
      video?.webkitEnterFullscreen?.()
    }
  }

  const session = detail?.session
  const reconnecting = session?.publisher_state === 'reconnecting'
  // After the stream has played once, any interruption is the broadcaster
  // briefly dropping out — say so instead of "connecting".
  const backSoon = hadPlayed && status !== 'ended' && status !== 'offline' && status !== 'notFound' &&
    (status === 'waiting' || status === 'connecting' || stalled || reconnecting)

  const overlay: Record<Status, string | null> = {
    loading: t('watch.connecting'),
    connecting: t('watch.connecting'),
    waiting: t('watch.waiting'),
    playing: null,
    ended: t('watch.ended'),
    offline: t('watch.offline', { name: detail?.owner_name || t('watch.someone') }),
    notFound: t('watch.notFound'),
    error: t('watch.error'),
  }
  const overlayText = backSoon ? t('watch.backSoon') : overlay[status]
  const battery = session?.battery_level
  const BatteryIcon = session?.battery_charging ? BatteryCharging : battery !== undefined && battery < 0.2 ? BatteryLow : Battery

  return (
    <div className="space-y-2">
      <div ref={containerRef} className="relative overflow-hidden rounded-xl bg-black">
        <video
          ref={videoRef}
          autoPlay
          playsInline
          muted={muted}
          onPlaying={() => {
            setHadPlayed(true)
            setStatus(s => (s === 'connecting' || s === 'waiting' ? 'playing' : s))
          }}
          className="w-full aspect-video max-h-[80vh] object-contain"
        />
        {overlayText && (
          <div className="absolute inset-0 flex items-center justify-center p-4 text-center text-gray-100" role="status">
            {backSoon && lastFrame && (
              <img src={lastFrame} alt="" className="absolute inset-0 h-full w-full object-contain opacity-60 blur-[2px]" />
            )}
            <span className={`relative rounded-lg px-3 py-2 ${backSoon ? 'bg-black/70' : 'bg-black/60'}`}>{overlayText}</span>
          </div>
        )}
        {status === 'playing' && !backSoon && (
          <div className="absolute top-2 left-2 flex items-center gap-2 text-xs font-medium">
            <span className="rounded bg-red-600 px-2 py-1 text-white">{t('list.onAir')}</span>
            {session && (
              <span className="flex items-center gap-1 rounded bg-black/60 px-2 py-1 text-white">
                <Eye size={14} />
                {session.viewers}
              </span>
            )}
            {session?.recording && (
              <span className="flex items-center gap-1 rounded bg-black/60 px-2 py-1 text-white">
                <span className="h-2 w-2 rounded-full bg-red-500" />
                {t('watch.recording')}
              </span>
            )}
          </div>
        )}
        {battery !== undefined && status !== 'ended' && status !== 'offline' && status !== 'notFound' && (
          <span
            className={`absolute top-2 right-2 flex items-center gap-1 rounded bg-black/60 px-2 py-1 text-xs font-medium ${battery < 0.2 && !session?.battery_charging ? 'text-red-400' : 'text-white'}`}
            aria-label={t('watch.battery', { percent: Math.round(battery * 100) })}
          >
            <BatteryIcon size={14} />
            {Math.round(battery * 100)}%
          </span>
        )}
        {status !== 'ended' && status !== 'offline' && status !== 'notFound' && (
          <div className="absolute bottom-2 right-2 flex gap-2">
            <button
              type="button"
              onClick={toggleMute}
              className="flex items-center gap-1.5 rounded-lg bg-black/70 px-3 py-2 text-sm text-white hover:bg-black/90"
            >
              {muted ? <VolumeX size={16} /> : <Volume2 size={16} />}
              {muted ? t('watch.unmute') : t('watch.mute')}
            </button>
            <button
              type="button"
              onClick={goFullscreen}
              aria-label={t('watch.fullscreen')}
              className="rounded-lg bg-black/70 px-3 py-2 text-white hover:bg-black/90"
            >
              <Maximize size={16} />
            </button>
          </div>
        )}
      </div>
      {mode === 'hls' && status !== 'ended' && <div className="text-sm text-gray-400">{t('watch.fallbackHls')}</div>}
    </div>
  )
}
