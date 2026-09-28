import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { ArrowLeft, Copy, Eye, Link2Off, Mic, MicOff, Radio, Share2, Square, SwitchCamera } from 'lucide-react'
import RunStatsBar from './live/RunStatsBar'
import {
  HttpError,
  computeRunStats,
  createSession,
  createShareLink,
  listSessions,
  postTrack,
  revokeShareLink,
  type TrackPoint,
  endSession,
  fetchIceServers,
  formatBytes,
  formatDuration,
  getSession,
  hangUp,
  heartbeat,
  negotiate,
  preferH264,
  sampleOutbound,
  sessionUrl,
  type LiveSession,
  type OutboundSample,
} from './live/liveApi'

type Phase = 'idle' | 'connecting' | 'live' | 'reconnecting'
type Facing = 'environment' | 'user'

// Capture/encode presets. Normal is ~0.5–0.7 GB/hour; low-data is for weak
// coverage. WebRTC congestion control degrades further on its own.
const QUALITY = {
  normal: { width: 1280, height: 720, fps: 30, bitrate: 1_500_000 },
  low: { width: 854, height: 480, fps: 24, bitrate: 600_000 },
} as const

const DEFAULT_HEARTBEAT_MS = 15_000
const STATS_INTERVAL_MS = 2_000
// A 'disconnected' ICE state often recovers by itself (cell handover); only
// tear down and renegotiate if it lasts longer than this.
const DISCONNECT_GRACE_MS = 5_000
const MAX_RECONNECT_DELAY_MS = 15_000
// GPS fixes are batched and uploaded this often while location sharing is on.
const TRACK_FLUSH_MS = 5_000
const MAX_TRACK_BUFFER = 1_000
// Auto low-data: switch when the encoder reports it is bandwidth-limited and
// the send rate stays under this for AUTO_LOW_STRIKES stats samples (~6 s).
const AUTO_LOW_BITRATE = 500_000
const AUTO_LOW_STRIKES = 5
// WebRTC's bandwidth estimate starts low and ramps up; the encoder reports
// "bandwidth-limited" during that ramp, so don't judge the first seconds.
const AUTO_LOW_WARMUP_MS = 20_000
const LOCATION_PREF_KEY = 'hytte-live-location'

function readLocationPref(): boolean {
  try {
    return localStorage.getItem(LOCATION_PREF_KEY) === '1'
  } catch {
    return false
  }
}

function writeLocationPref(on: boolean) {
  try {
    localStorage.setItem(LOCATION_PREF_KEY, on ? '1' : '0')
  } catch {
    // Private mode etc. — the checkbox just won't be remembered.
  }
}

interface BatteryManagerLike extends EventTarget {
  level: number
  charging: boolean
}

export default function LiveBroadcast() {
  const { t, i18n } = useTranslation('livestream')

  const videoRef = useRef<HTMLVideoElement>(null)
  const streamRef = useRef<MediaStream | null>(null)
  const pcRef = useRef<RTCPeerConnection | null>(null)
  const videoSenderRef = useRef<RTCRtpSender | null>(null)
  const resourceRef = useRef<string | null>(null)
  const sessionRef = useRef<LiveSession | null>(null)
  const wakeLockRef = useRef<WakeLockSentinel | null>(null)
  // activeRef is the user's intent: true from Start until Stop (or the
  // server ends the session). Reconnect logic only runs while it is set.
  const activeRef = useRef(false)
  const attemptRef = useRef(0)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const disconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const lowDataRef = useRef(false)
  const micOnRef = useRef(true)
  // Bytes sent by peer connections that were torn down during reconnects, so
  // the data-used readout covers the whole broadcast.
  const baseBytesRef = useRef(0)
  const lastSampleRef = useRef<OutboundSample | null>(null)
  const heartbeatMsRef = useRef(DEFAULT_HEARTBEAT_MS)
  const connectRef = useRef<() => Promise<void>>(async () => {})
  const phaseRef = useRef<Phase>('idle')
  const batteryRef = useRef<{ level: number; charging: boolean } | null>(null)
  const trackBufferRef = useRef<TrackPoint[]>([])
  const geoWatchRef = useRef<number | null>(null)
  const bandwidthStrikesRef = useRef(0)
  const connectedAtRef = useRef(0)
  const recordingRef = useRef(false)

  const [facing, setFacing] = useState<Facing>('environment')
  const [micOn, setMicOn] = useState(true)
  const [lowData, setLowData] = useState(false)
  const [title, setTitle] = useState('')
  const [phase, setPhase] = useState<Phase>('idle')
  const [cameraError, setCameraError] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [startedAt, setStartedAt] = useState<number | null>(null)
  const [now, setNow] = useState(() => Date.now())
  const [bitrate, setBitrate] = useState(0)
  const [bytesUsed, setBytesUsed] = useState(0)
  const [viewers, setViewers] = useState(0)
  // Go live options.
  const [notify, setNotify] = useState(true)
  const [record, setRecord] = useState(false)
  const [shareLocation, setShareLocation] = useState(readLocationPref)
  const [recordingAvailable, setRecordingAvailable] = useState(false)
  // State of the running broadcast.
  const [isRecording, setIsRecording] = useState(false)
  const [locationOn, setLocationOn] = useState(false)
  const [myPoints, setMyPoints] = useState<TrackPoint[]>([])
  const [shareUrl, setShareUrl] = useState<string | null>(null)
  const [geoDenied, setGeoDenied] = useState(false)
  const wakeLockSupported = typeof navigator !== 'undefined' && 'wakeLock' in navigator

  // --- camera -------------------------------------------------------------

  const openCamera = useCallback(async (facingMode: Facing, low: boolean) => {
    const q = low ? QUALITY.low : QUALITY.normal
    const old = streamRef.current
    // Phones often cannot open two cameras at once: release the old one first.
    old?.getVideoTracks().forEach(track => track.stop())
    const needAudio = !old || old.getAudioTracks().length === 0
    const media = await navigator.mediaDevices.getUserMedia({
      video: {
        facingMode: { ideal: facingMode },
        width: { ideal: q.width },
        height: { ideal: q.height },
        frameRate: { ideal: q.fps, max: 30 },
      },
      audio: needAudio ? { echoCancellation: true, noiseSuppression: true, autoGainControl: true } : false,
    })
    const videoTrack = media.getVideoTracks()[0]
    if ('contentHint' in videoTrack) videoTrack.contentHint = 'motion'
    const audioTracks = needAudio ? media.getAudioTracks() : (old?.getAudioTracks() ?? [])
    audioTracks.forEach(track => { track.enabled = micOnRef.current })
    const stream = new MediaStream([videoTrack, ...audioTracks])
    streamRef.current = stream
    if (videoRef.current) videoRef.current.srcObject = stream
    // Swap the camera on a live connection without renegotiating.
    if (videoSenderRef.current) {
      await videoSenderRef.current.replaceTrack(videoTrack)
    }
  }, [])

  const describeCameraError = useCallback((err: unknown) => {
    if (err instanceof DOMException && (err.name === 'NotAllowedError' || err.name === 'SecurityError')) {
      return t('broadcast.cameraDenied')
    }
    return t('broadcast.cameraError', { message: err instanceof Error ? err.message : String(err) })
  }, [t])

  useEffect(() => {
    let cancelled = false
    openCamera('environment', false).catch(err => {
      if (!cancelled) setCameraError(describeCameraError(err))
    })
    return () => {
      cancelled = true
    }
  }, [openCamera, describeCameraError])

  const flipCamera = async () => {
    const next: Facing = facing === 'environment' ? 'user' : 'environment'
    try {
      await openCamera(next, lowDataRef.current)
      setFacing(next)
      setCameraError(null)
    } catch (err) {
      setCameraError(describeCameraError(err))
    }
  }

  const toggleMic = () => {
    const next = !micOn
    micOnRef.current = next
    setMicOn(next)
    streamRef.current?.getAudioTracks().forEach(track => { track.enabled = next })
  }

  const applyBitrate = useCallback(async (low: boolean) => {
    const sender = videoSenderRef.current
    if (!sender) return
    const q = low ? QUALITY.low : QUALITY.normal
    try {
      const params = sender.getParameters()
      if (!params.encodings || params.encodings.length === 0) params.encodings = [{}]
      params.encodings[0].maxBitrate = q.bitrate
      params.encodings[0].maxFramerate = q.fps
      await sender.setParameters(params)
    } catch {
      // Not every browser allows changing encodings mid-call; the capture
      // constraints below still reduce the bitrate.
    }
  }, [])

  const setLowDataMode = useCallback(async (next: boolean) => {
    lowDataRef.current = next
    setLowData(next)
    const q = next ? QUALITY.low : QUALITY.normal
    const track = streamRef.current?.getVideoTracks()[0]
    try {
      await track?.applyConstraints({ width: { ideal: q.width }, height: { ideal: q.height }, frameRate: { ideal: q.fps, max: 30 } })
    } catch {
      // Constraint changes are best effort.
    }
    await applyBitrate(next)
  }, [applyBitrate])

  const toggleLowData = () => setLowDataMode(!lowDataRef.current)

  // --- server capabilities + battery ---------------------------------------

  useEffect(() => {
    let cancelled = false
    listSessions()
      .then(d => { if (!cancelled) setRecordingAvailable(d.recording_available) })
      .catch(() => {})
    // Battery Status API: Chrome/Android only. Viewers see the level so they
    // know when the phone is about to die.
    const nav = navigator as Navigator & { getBattery?: () => Promise<BatteryManagerLike> }
    let battery: BatteryManagerLike | null = null
    const update = () => {
      if (battery) batteryRef.current = { level: battery.level, charging: battery.charging }
    }
    nav.getBattery?.().then(b => {
      if (cancelled) return
      battery = b
      update()
      b.addEventListener('levelchange', update)
      b.addEventListener('chargingchange', update)
    }).catch(() => {})
    return () => {
      cancelled = true
      battery?.removeEventListener('levelchange', update)
      battery?.removeEventListener('chargingchange', update)
    }
  }, [])

  useEffect(() => {
    phaseRef.current = phase
  }, [phase])

  // --- GPS ---------------------------------------------------------------------

  const stopGeo = useCallback(() => {
    if (geoWatchRef.current !== null) navigator.geolocation?.clearWatch(geoWatchRef.current)
    geoWatchRef.current = null
    trackBufferRef.current = []
  }, [])

  const startGeo = useCallback(() => {
    if (!navigator.geolocation || geoWatchRef.current !== null) return
    geoWatchRef.current = navigator.geolocation.watchPosition(
      pos => {
        const point: TrackPoint = {
          t: new Date(pos.timestamp).toISOString(),
          lat: pos.coords.latitude,
          lon: pos.coords.longitude,
          acc: pos.coords.accuracy,
          ...(pos.coords.altitude !== null ? { alt: pos.coords.altitude } : {}),
        }
        trackBufferRef.current.push(point)
        if (trackBufferRef.current.length > MAX_TRACK_BUFFER) trackBufferRef.current.shift()
        setMyPoints(prev => [...prev, point])
        setGeoDenied(false)
      },
      err => {
        // Timeouts and "position unavailable" are routine (tunnels, tall
        // buildings) and the watch keeps going — only a refusal needs the user.
        if (err.code === err.PERMISSION_DENIED) setGeoDenied(true)
      },
      { enableHighAccuracy: true, maximumAge: 0, timeout: 20_000 },
    )
  }, [])

  // --- wake lock ----------------------------------------------------------

  const acquireWakeLock = useCallback(async () => {
    if (!('wakeLock' in navigator) || wakeLockRef.current) return
    try {
      const lock = await navigator.wakeLock.request('screen')
      wakeLockRef.current = lock
      lock.addEventListener('release', () => {
        if (wakeLockRef.current === lock) wakeLockRef.current = null
      })
    } catch {
      // Denied (e.g. low battery mode) — the on-page tip covers it.
    }
  }, [])

  const releaseWakeLock = useCallback(() => {
    wakeLockRef.current?.release().catch(() => {})
    wakeLockRef.current = null
  }, [])

  useEffect(() => {
    // The browser drops the wake lock whenever the page is hidden; take it
    // back as soon as the broadcaster returns to the tab.
    const onVisible = () => {
      if (document.visibilityState === 'visible' && activeRef.current) acquireWakeLock()
    }
    document.addEventListener('visibilitychange', onVisible)
    return () => document.removeEventListener('visibilitychange', onVisible)
  }, [acquireWakeLock])

  // --- connection ---------------------------------------------------------

  const clearTimers = () => {
    if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current)
    if (disconnectTimerRef.current) clearTimeout(disconnectTimerRef.current)
    reconnectTimerRef.current = null
    disconnectTimerRef.current = null
  }

  const teardownPeer = useCallback(() => {
    const pc = pcRef.current
    if (!pc) return
    pcRef.current = null
    videoSenderRef.current = null
    if (lastSampleRef.current) baseBytesRef.current += lastSampleRef.current.bytes
    lastSampleRef.current = null
    hangUp(resourceRef.current)
    resourceRef.current = null
    pc.close()
  }, [])

  const finish = useCallback((endedNotice: string | null) => {
    activeRef.current = false
    clearTimers()
    teardownPeer()
    releaseWakeLock()
    stopGeo()
    sessionRef.current = null
    setShareUrl(null)
    setLocationOn(false)
    if (recordingRef.current && endedNotice) {
      endedNotice = `${endedNotice} ${t('broadcast.replaySoon')}`
    }
    recordingRef.current = false
    setIsRecording(false)
    setPhase('idle')
    setStartedAt(null)
    setBitrate(0)
    setViewers(0)
    setNotice(endedNotice)
  }, [teardownPeer, releaseWakeLock, stopGeo, t])

  const scheduleReconnect = useCallback(() => {
    if (!activeRef.current) return
    teardownPeer()
    setPhase('reconnecting')
    if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current)
    const delay = Math.min(MAX_RECONNECT_DELAY_MS, 1000 * 2 ** attemptRef.current)
    attemptRef.current += 1
    reconnectTimerRef.current = setTimeout(() => {
      reconnectTimerRef.current = null
      connectRef.current()
    }, delay)
  }, [teardownPeer])

  const connect = useCallback(async () => {
    const session = sessionRef.current
    const stream = streamRef.current
    if (!activeRef.current || !session || !stream) return
    const iceServers = await fetchIceServers()
    if (!activeRef.current) return

    const q = lowDataRef.current ? QUALITY.low : QUALITY.normal
    const videoTrack = stream.getVideoTracks()[0]
    const audioTrack = stream.getAudioTracks()[0]
    let pc: RTCPeerConnection
    let videoTx: RTCRtpTransceiver
    try {
      pc = new RTCPeerConnection({ iceServers })
      videoTx = pc.addTransceiver(videoTrack, {
        direction: 'sendonly',
        streams: [stream],
        sendEncodings: [{ maxBitrate: q.bitrate, maxFramerate: q.fps }],
      })
      if (audioTrack) pc.addTransceiver(audioTrack, { direction: 'sendonly', streams: [stream] })
    } catch (err) {
      // WebRTC unavailable: retrying will not help, so end the session.
      const failed = sessionRef.current
      finish(t('broadcast.startError', { message: err instanceof Error ? err.message : String(err) }))
      if (failed) endSession(failed.id).catch(() => {})
      return
    }
    preferH264(videoTx)
    pcRef.current = pc
    videoSenderRef.current = videoTx.sender

    pc.addEventListener('connectionstatechange', () => {
      if (pcRef.current !== pc) return
      switch (pc.connectionState) {
        case 'connected':
          if (disconnectTimerRef.current) clearTimeout(disconnectTimerRef.current)
          disconnectTimerRef.current = null
          attemptRef.current = 0
          connectedAtRef.current = performance.now()
          bandwidthStrikesRef.current = 0
          setPhase('live')
          break
        case 'disconnected':
          setPhase('reconnecting')
          if (!disconnectTimerRef.current) {
            disconnectTimerRef.current = setTimeout(() => {
              disconnectTimerRef.current = null
              if (pcRef.current === pc && pc.connectionState !== 'connected') scheduleReconnect()
            }, DISCONNECT_GRACE_MS)
          }
          break
        case 'failed':
          scheduleReconnect()
          break
      }
    })

    try {
      resourceRef.current = await negotiate(pc, sessionUrl(session.id, '/whip'))
    } catch (err) {
      if (pcRef.current !== pc) return
      if (err instanceof HttpError && (err.status === 404 || err.status === 410)) {
        finish(t('broadcast.endedRemotely'))
        return
      }
      scheduleReconnect()
      return
    }
    if (pcRef.current !== pc) {
      // Stopped or superseded while negotiating.
      return
    }
    try {
      const params = videoTx.sender.getParameters() as RTCRtpSendParameters & { degradationPreference?: string }
      params.degradationPreference = 'maintain-framerate'
      await videoTx.sender.setParameters(params)
    } catch {
      // Optional hint; ignore where unsupported.
    }
  }, [finish, scheduleReconnect, t])

  useEffect(() => {
    connectRef.current = connect
  }, [connect])

  useEffect(() => {
    // Coming back from a dead zone: skip the remaining backoff.
    const onOnline = () => {
      if (activeRef.current && reconnectTimerRef.current) {
        clearTimeout(reconnectTimerRef.current)
        reconnectTimerRef.current = null
        attemptRef.current = 0
        connectRef.current()
      }
    }
    window.addEventListener('online', onOnline)
    return () => window.removeEventListener('online', onOnline)
  }, [])

  const start = async () => {
    if (!streamRef.current) return
    setError(null)
    setNotice(null)
    setPhase('connecting')
    activeRef.current = true
    attemptRef.current = 0
    baseBytesRef.current = 0
    lastSampleRef.current = null
    setBytesUsed(0)
    setMyPoints([])
    writeLocationPref(shareLocation)
    try {
      const res = await createSession(title.trim(), { notify, record, location: shareLocation })
      sessionRef.current = res.session
      heartbeatMsRef.current = (res.heartbeat_interval || 15) * 1000
      recordingRef.current = res.session.recording
      setIsRecording(res.session.recording)
      setLocationOn(res.session.location)
      if (res.session.location) startGeo()
    } catch (err) {
      activeRef.current = false
      setPhase('idle')
      if (err instanceof HttpError && err.status === 507) {
        setError(t('broadcast.lowDisk'))
      } else if (err instanceof HttpError && err.status === 503 && record) {
        setError(t('broadcast.recordUnavailable'))
      } else {
        setError(t('broadcast.startError', { message: err instanceof Error ? err.message : String(err) }))
      }
      return
    }
    setStartedAt(Date.now())
    setNow(Date.now())
    acquireWakeLock()
    await connect()
  }

  const stop = async () => {
    const session = sessionRef.current
    finish(t('broadcast.ended'))
    if (session) {
      try {
        await endSession(session.id)
      } catch {
        // The heartbeat reaper ends it within a couple of minutes anyway.
      }
    }
  }

  // --- while live: heartbeat, stats, clock --------------------------------

  const isActive = phase !== 'idle'

  useEffect(() => {
    if (!isActive) return
    const beat = async () => {
      const session = sessionRef.current
      if (!session) return
      try {
        const battery = batteryRef.current
        const state = phaseRef.current === 'live' || phaseRef.current === 'reconnecting' ? phaseRef.current : 'connecting'
        await heartbeat(session.id, {
          state,
          ...(battery ? { battery_level: battery.level, battery_charging: battery.charging } : {}),
        })
        const fresh = await getSession(session.id)
        setViewers(fresh.viewers)
      } catch (err) {
        if (err instanceof HttpError && (err.status === 404 || err.status === 409)) {
          finish(t('broadcast.endedRemotely'))
        }
        // Network errors: keep going; the reconnect logic handles media.
      }
    }
    const id = setInterval(beat, heartbeatMsRef.current)
    return () => clearInterval(id)
  }, [isActive, finish, t])

  useEffect(() => {
    if (!isActive) return
    const id = setInterval(async () => {
      setNow(Date.now())
      const pc = pcRef.current
      if (!pc) {
        setBitrate(0)
        return
      }
      try {
        const sample = await sampleOutbound(pc)
        const prev = lastSampleRef.current
        if (prev && sample.at > prev.at && sample.bytes >= prev.bytes) {
          const rate = ((sample.bytes - prev.bytes) * 8) / ((sample.at - prev.at) / 1000)
          setBitrate(rate)
          // Weak coverage: drop to low-data before the picture falls apart.
          const warmedUp = connectedAtRef.current > 0 && performance.now() - connectedAtRef.current > AUTO_LOW_WARMUP_MS
          if (!lowDataRef.current && warmedUp && sample.bandwidthLimited && rate < AUTO_LOW_BITRATE) {
            bandwidthStrikesRef.current += 1
            if (bandwidthStrikesRef.current >= AUTO_LOW_STRIKES) {
              bandwidthStrikesRef.current = 0
              setLowDataMode(true)
              setNotice(t('broadcast.autoLowData'))
            }
          } else {
            bandwidthStrikesRef.current = 0
          }
        }
        lastSampleRef.current = sample
        setBytesUsed(baseBytesRef.current + sample.bytes)
      } catch {
        // getStats can throw on a closing connection.
      }
    }, STATS_INTERVAL_MS)
    return () => clearInterval(id)
  }, [isActive, setLowDataMode, t])

  // Upload buffered GPS fixes.
  useEffect(() => {
    if (!isActive || !locationOn) return
    const id = setInterval(async () => {
      const session = sessionRef.current
      if (!session || trackBufferRef.current.length === 0) return
      const batch = trackBufferRef.current.splice(0, 200)
      try {
        await postTrack(session.id, batch)
      } catch {
        // Keep them for the next attempt (tunnels, dead zones).
        trackBufferRef.current.unshift(...batch)
        trackBufferRef.current.splice(MAX_TRACK_BUFFER)
      }
    }, TRACK_FLUSH_MS)
    return () => clearInterval(id)
  }, [isActive, locationOn])

  // --- share link -------------------------------------------------------------

  const share = async () => {
    const session = sessionRef.current
    if (!session) return
    let url: string
    try {
      const { path } = await createShareLink(session.id)
      url = window.location.origin + path
      setShareUrl(url)
    } catch {
      setError(t('broadcast.shareError'))
      return
    }
    // The link exists now; handing it off is best effort (the field below
    // shows it either way).
    if (navigator.share) {
      await navigator.share({ title: title.trim() || t('broadcast.shareTitle'), url }).catch(() => {})
    } else {
      await copyText(url)
    }
  }

  const copyText = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setNotice(t('broadcast.linkCopied'))
    } catch {
      // Clipboard blocked — the link is visible and selectable anyway.
    }
  }

  const copyShareUrl = () => {
    if (shareUrl) copyText(shareUrl)
  }

  const stopSharing = async () => {
    const session = sessionRef.current
    if (!session) return
    try {
      await revokeShareLink(session.id)
      setShareUrl(null)
    } catch {
      setError(t('broadcast.shareError'))
    }
  }

  // --- page exit ----------------------------------------------------------

  useEffect(() => {
    const endOnExit = () => {
      const session = sessionRef.current
      if (activeRef.current && session) navigator.sendBeacon?.(sessionUrl(session.id, '/end'))
    }
    window.addEventListener('pagehide', endOnExit)
    return () => {
      window.removeEventListener('pagehide', endOnExit)
      // Navigating away inside the app: end the broadcast and free the camera.
      endOnExit()
      activeRef.current = false
      clearTimers()
      const pc = pcRef.current
      pcRef.current = null
      hangUp(resourceRef.current)
      pc?.close()
      if (geoWatchRef.current !== null) navigator.geolocation?.clearWatch(geoWatchRef.current)
      wakeLockRef.current?.release().catch(() => {})
      streamRef.current?.getTracks().forEach(track => track.stop())
    }
  }, [])

  // --- render -------------------------------------------------------------

  const locale = i18n.language
  const kbps = Math.round(bitrate / 1000)
  const elapsed = startedAt ? (now - startedAt) / 1000 : 0
  const statusText = t(`broadcast.status.${phase}`)

  return (
    <div className="p-4 md:p-6 max-w-3xl mx-auto space-y-4">
      <div className="flex items-center justify-between gap-2">
        <h1 className="text-xl font-semibold text-white flex items-center gap-2">
          <Radio size={22} className="text-red-500" />
          {t('broadcast.title')}
        </h1>
        <Link to="/live" className="flex items-center gap-1.5 text-sm text-gray-400 hover:text-white">
          <ArrowLeft size={16} />
          {t('broadcast.backToList')}
        </Link>
      </div>

      <div className="relative overflow-hidden rounded-xl bg-black">
        <video
          ref={videoRef}
          autoPlay
          playsInline
          muted
          className={`w-full aspect-video max-h-[60vh] object-cover ${facing === 'user' ? '-scale-x-100' : ''}`}
        />
        {isActive && (
          <div className="absolute top-2 left-2 right-2 flex flex-wrap items-center gap-2 text-xs font-medium">
            <span
              className={`flex items-center gap-1.5 rounded px-2 py-1 text-white ${phase === 'live' ? 'bg-red-600' : 'bg-yellow-600'}`}
              role="status"
            >
              <span className={`h-2 w-2 rounded-full bg-white ${phase === 'live' ? 'animate-pulse' : ''}`} />
              {statusText} · {formatDuration(elapsed)}
            </span>
            <span className="flex items-center gap-1 rounded bg-black/60 px-2 py-1 text-white">
              <Eye size={14} />
              {viewers}
            </span>
            {isRecording && (
              <span className="flex items-center gap-1 rounded bg-black/60 px-2 py-1 text-white">
                <span className="h-2 w-2 rounded-full bg-red-500" />
                {t('broadcast.rec')}
              </span>
            )}
            <span className="rounded bg-black/60 px-2 py-1 text-white">{t('broadcast.bitrate', { kbps })}</span>
            <span className="rounded bg-black/60 px-2 py-1 text-white">
              {t('broadcast.dataUsed', { amount: formatBytes(bytesUsed, locale) })}
            </span>
          </div>
        )}
        {!micOn && (
          <span className="absolute bottom-2 left-2 flex items-center gap-1 rounded bg-black/60 px-2 py-1 text-xs text-white">
            <MicOff size={14} />
            {t('broadcast.muted')}
          </span>
        )}
      </div>

      {cameraError && <div className="text-sm text-red-400" role="alert">{cameraError}</div>}
      {error && <div className="text-sm text-red-400" role="alert">{error}</div>}
      {geoDenied && locationOn && <div className="text-sm text-red-400" role="alert">{t('broadcast.locationError')}</div>}
      {notice && <div className="text-sm text-gray-300" role="status">{notice}</div>}

      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={flipCamera}
          className="flex items-center gap-1.5 rounded-lg bg-gray-800 px-3 py-2 text-sm text-gray-200 hover:bg-gray-700"
        >
          <SwitchCamera size={16} />
          {t('broadcast.flipCamera')}
        </button>
        <button
          type="button"
          onClick={toggleMic}
          aria-pressed={!micOn}
          className={`flex items-center gap-1.5 rounded-lg px-3 py-2 text-sm ${micOn ? 'bg-gray-800 text-gray-200 hover:bg-gray-700' : 'bg-yellow-700 text-white hover:bg-yellow-600'}`}
        >
          {micOn ? <Mic size={16} /> : <MicOff size={16} />}
          {micOn ? t('broadcast.micOn') : t('broadcast.micOff')}
        </button>
        <label className="flex items-center gap-2 rounded-lg bg-gray-800 px-3 py-2 text-sm text-gray-200">
          <input type="checkbox" checked={lowData} onChange={toggleLowData} className="accent-blue-500" />
          <span>
            {t('broadcast.lowData')}
            <span className="hidden sm:inline text-gray-400"> — {t('broadcast.lowDataHint')}</span>
          </span>
        </label>
      </div>

      {isActive && (
        <div className="space-y-2">
          {shareUrl ? (
            <div className="flex flex-wrap items-center gap-2 rounded-lg bg-gray-800 p-2">
              <input
                readOnly
                value={shareUrl}
                aria-label={t('broadcast.shareLinkLabel')}
                onFocus={e => e.currentTarget.select()}
                className="min-w-0 flex-1 rounded bg-gray-900 px-2 py-1.5 text-xs text-gray-200"
              />
              <button type="button" onClick={copyShareUrl} className="flex items-center gap-1.5 rounded-lg bg-gray-700 px-3 py-1.5 text-sm text-gray-100 hover:bg-gray-600">
                <Copy size={14} />
                {t('broadcast.copyLink')}
              </button>
              <button type="button" onClick={stopSharing} className="flex items-center gap-1.5 rounded-lg bg-gray-700 px-3 py-1.5 text-sm text-red-300 hover:bg-gray-600">
                <Link2Off size={14} />
                {t('broadcast.stopSharing')}
              </button>
            </div>
          ) : (
            <button type="button" onClick={share} className="flex items-center gap-1.5 rounded-lg bg-gray-800 px-3 py-2 text-sm text-gray-200 hover:bg-gray-700">
              <Share2 size={16} />
              {t('broadcast.shareLink')}
            </button>
          )}
          {locationOn && myPoints.length > 1 && <RunStatsBar stats={computeRunStats(myPoints)} />}
        </div>
      )}

      {!isActive && (
        <fieldset className="space-y-2">
          <legend className="sr-only">{t('broadcast.optionsLabel')}</legend>
          <label className="flex items-start gap-3 rounded-lg bg-gray-800 px-3 py-2 text-sm text-gray-200">
            <input type="checkbox" checked={notify} onChange={e => setNotify(e.target.checked)} className="mt-0.5 accent-blue-500" />
            <span>
              {t('broadcast.optNotify')}
              <span className="block text-xs text-gray-400">{t('broadcast.optNotifyHint')}</span>
            </span>
          </label>
          <label className={`flex items-start gap-3 rounded-lg bg-gray-800 px-3 py-2 text-sm ${recordingAvailable ? 'text-gray-200' : 'text-gray-500'}`}>
            <input
              type="checkbox"
              checked={record && recordingAvailable}
              disabled={!recordingAvailable}
              onChange={e => setRecord(e.target.checked)}
              className="mt-0.5 accent-blue-500"
            />
            <span>
              {t('broadcast.optRecord')}
              <span className="block text-xs text-gray-400">
                {recordingAvailable ? t('broadcast.optRecordHint') : t('broadcast.recordUnavailable')}
              </span>
            </span>
          </label>
          <label className="flex items-start gap-3 rounded-lg bg-gray-800 px-3 py-2 text-sm text-gray-200">
            <input type="checkbox" checked={shareLocation} onChange={e => setShareLocation(e.target.checked)} className="mt-0.5 accent-blue-500" />
            <span>
              {t('broadcast.optLocation')}
              <span className="block text-xs text-gray-400">{t('broadcast.optLocationHint')}</span>
            </span>
          </label>
        </fieldset>
      )}

      {!isActive && (
        <div className="space-y-1">
          <label htmlFor="live-title" className="text-sm text-gray-400">{t('broadcast.titleLabel')}</label>
          <input
            id="live-title"
            type="text"
            value={title}
            maxLength={120}
            onChange={e => setTitle(e.target.value)}
            placeholder={t('broadcast.titlePlaceholder')}
            className="w-full rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-white placeholder-gray-500 focus:border-blue-500 focus:outline-none"
          />
        </div>
      )}

      {isActive ? (
        <button
          type="button"
          onClick={stop}
          className="flex w-full items-center justify-center gap-2 rounded-xl bg-gray-700 py-4 text-lg font-semibold text-white hover:bg-gray-600"
        >
          <Square size={20} />
          {t('broadcast.stop')}
        </button>
      ) : (
        <button
          type="button"
          onClick={start}
          disabled={!!cameraError}
          className="flex w-full items-center justify-center gap-2 rounded-xl bg-red-600 py-4 text-lg font-semibold text-white hover:bg-red-500 disabled:cursor-not-allowed disabled:opacity-50"
        >
          <Radio size={20} />
          {t('broadcast.start')}
        </button>
      )}

      <ul className="space-y-1 text-sm text-gray-400 list-disc pl-5">
        <li>{t('broadcast.tipScreen')}</li>
        {!wakeLockSupported && <li>{t('broadcast.wakeLockUnsupported')}</li>}
        <li>{t('broadcast.dataEstimate')}</li>
      </ul>
    </div>
  )
}
