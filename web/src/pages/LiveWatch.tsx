import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useParams } from 'react-router'
import { ArrowLeft, Eye, Maximize, Volume2, VolumeX } from 'lucide-react'
import type Hls from 'hls.js'
import { HttpError, fetchIceServers, getSession, hangUp, negotiate, sessionUrl, type LiveSession } from './live/liveApi'

type Status = 'loading' | 'waiting' | 'connecting' | 'playing' | 'ended' | 'notFound' | 'error'

const POLL_MS = 10_000
// Broadcaster not on air yet (MediaMTX answers WHEP with 404): retry soon.
const WAITING_RETRY_MS = 3_000
// If WebRTC has not connected in this long, the viewer's network is probably
// blocking it — fall back to HLS.
const CONNECT_TIMEOUT_MS = 12_000
// Consecutive WebRTC failures (connection 'failed' or a non-404 signalling
// error) before switching to HLS.
const MAX_WEBRTC_FAILURES = 2

export default function LiveWatch() {
  const { t } = useTranslation('livestream')
  const { id } = useParams()
  const sessionId = Number(id)
  const validId = Number.isInteger(sessionId) && sessionId > 0

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

  const [session, setSession] = useState<LiveSession | null>(null)
  const [status, setStatus] = useState<Status>('loading')
  const [mode, setMode] = useState<'webrtc' | 'hls'>('webrtc')
  const [muted, setMuted] = useState(true)

  const stopPlayback = useCallback(() => {
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
  }, [])

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
    const url = sessionUrl(sessionId, '/hls/index.m3u8')
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
      hls.loadSource(url)
      hls.attachMedia(video)
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = url
    } else {
      setStatus('error')
      return
    }
    video.play().catch(() => {})
  }, [sessionId, stopPlayback, retryLater])

  const startWebRTC = useCallback(async () => {
    const video = videoRef.current
    if (!video || !activeRef.current) return
    stopPlayback()
    setStatus(s => (s === 'waiting' ? 'waiting' : 'connecting'))
    const iceServers = await fetchIceServers()
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
      } else if (pc.connectionState === 'failed') {
        // Could be the broadcaster reconnecting or our network: retry a
        // couple of times, then assume WebRTC is blocked and use HLS.
        failuresRef.current += 1
        if (failuresRef.current >= MAX_WEBRTC_FAILURES) {
          startHLS()
          return
        }
        setStatus('connecting')
        retryLater(WAITING_RETRY_MS)
      }
    })

    try {
      resourceRef.current = await negotiate(pc, sessionUrl(sessionId, '/whep'))
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
  }, [sessionId, stopPlayback, retryLater, startHLS])

  useEffect(() => {
    startRef.current = mode === 'hls' ? startHLS : startWebRTC
  }, [mode, startHLS, startWebRTC])

  // Initial load + periodic poll for status/viewers/ended.
  useEffect(() => {
    activeRef.current = true
    if (!validId) return
    let started = false
    const poll = async () => {
      try {
        const s = await getSession(sessionId)
        if (!activeRef.current) return
        setSession(s)
        if (s.status === 'ended') {
          stopPlayback()
          setStatus('ended')
          return
        }
        if (!started) {
          started = true
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
  }, [sessionId, validId, startWebRTC, stopPlayback])

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

  const overlay: Record<Status, string | null> = {
    loading: t('watch.connecting'),
    connecting: t('watch.connecting'),
    waiting: t('watch.waiting'),
    playing: null,
    ended: t('watch.ended'),
    notFound: t('watch.notFound'),
    error: t('watch.error'),
  }
  const overlayText = overlay[validId ? status : 'notFound']

  return (
    <div className="p-4 md:p-6 max-w-4xl mx-auto space-y-4">
      <div className="flex items-center justify-between gap-2">
        <div className="min-w-0">
          <h1 className="truncate text-xl font-semibold text-white">{session?.title || t('list.untitled')}</h1>
          {session && <div className="truncate text-sm text-gray-400">{session.owner_name}</div>}
        </div>
        <Link to="/live" className="flex shrink-0 items-center gap-1.5 text-sm text-gray-400 hover:text-white">
          <ArrowLeft size={16} />
          {t('watch.back')}
        </Link>
      </div>

      <div ref={containerRef} className="relative overflow-hidden rounded-xl bg-black">
        <video
          ref={videoRef}
          autoPlay
          playsInline
          muted={muted}
          onPlaying={() => setStatus(s => (s === 'connecting' || s === 'waiting' ? 'playing' : s))}
          className="w-full aspect-video max-h-[80vh] object-contain" />
        {overlayText && (
          <div className="absolute inset-0 flex items-center justify-center bg-black/60 p-4 text-center text-gray-200" role="status">
            {overlayText}
          </div>
        )}
        {status === 'playing' && (
          <div className="absolute top-2 left-2 flex items-center gap-2 text-xs font-medium">
            <span className="rounded bg-red-600 px-2 py-1 text-white">{t('list.onAir')}</span>
            {session && (
              <span className="flex items-center gap-1 rounded bg-black/60 px-2 py-1 text-white">
                <Eye size={14} />
                {session.viewers}
              </span>
            )}
          </div>
        )}
        {status !== 'ended' && status !== 'notFound' && (
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
