// Shared API + WebRTC helpers for the livestream pages (Hytte-krwxy).
//
// Signalling is plain WHIP/WHEP over Hytte's /api/live proxy: POST an SDP
// offer, get an SDP answer plus a Location for the session resource, DELETE
// that resource to hang up. We wait (bounded) for ICE gathering to finish
// before POSTing instead of trickling candidates — one round trip, and it
// works the same through every proxy hop.

export interface LiveSession {
  id: number
  user_id: number
  owner_name: string
  title: string
  status: 'live' | 'ended'
  started_at: string
  ended_at?: string
  is_owner: boolean
  on_air: boolean
  viewers: number
  recording: boolean
  location: boolean
  publisher_state?: '' | 'connecting' | 'live' | 'reconnecting'
  battery_level?: number
  battery_charging?: boolean
  has_share_link?: boolean
}

export interface LiveOptions {
  notify: boolean
  record: boolean
  location: boolean
}

export interface TrackPoint {
  id?: number
  t: string
  lat: number
  lon: number
  alt?: number
  acc?: number
}

export interface Recording {
  id: number
  session_id: number
  owner_name: string
  title: string
  started_at: string
  ended_at?: string
  status: 'recording' | 'processing' | 'ready' | 'failed'
  error?: string
  duration_seconds: number
  size_bytes: number
  is_owner: boolean
  has_track: boolean
  workout?: { id: number; title: string; sport: string; distance_meters: number; linkable: boolean }
}

/**
 * ViewerEndpoints is everything a viewer needs for one stream. Members use
 * /api/live/sessions/{id}/…; share-link visitors use /api/live/public/{token}/….
 */
export interface ViewerEndpoints {
  session: string
  whep: string
  hls: string
  ice: string
  track: string
}

export const memberEndpoints = (id: number): ViewerEndpoints => ({
  session: `/api/live/sessions/${id}`,
  whep: `/api/live/sessions/${id}/whep`,
  hls: `/api/live/sessions/${id}/hls/index.m3u8`,
  ice: '/api/live/ice',
  track: `/api/live/sessions/${id}/track`,
})

export const publicEndpoints = (token: string): ViewerEndpoints => ({
  session: `/api/live/public/${token}`,
  whep: `/api/live/public/${token}/whep`,
  hls: `/api/live/public/${token}/hls/index.m3u8`,
  ice: `/api/live/public/${token}/ice`,
  track: `/api/live/public/${token}/track`,
})

export interface LiveList {
  sessions: LiveSession[]
  configured: boolean
  recording_available: boolean
  heartbeat_interval: number
}

export class HttpError extends Error {
  status: number
  constructor(status: number, message?: string) {
    super(message ?? `HTTP ${status}`)
    this.status = status
  }
}

async function json<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let msg: string | undefined
    try {
      msg = (await res.json()).error
    } catch {
      // non-JSON error body
    }
    throw new HttpError(res.status, msg)
  }
  return res.json() as Promise<T>
}

export const sessionUrl = (id: number, suffix = '') => `/api/live/sessions/${id}${suffix}`

export function listSessions(): Promise<LiveList> {
  return fetch('/api/live/sessions', { credentials: 'include' }).then(r => json<LiveList>(r))
}

export interface SessionDetail {
  session: LiveSession
  recording_id?: number
  recording_status?: Recording['status']
}

export function getSessionDetail(url: string): Promise<SessionDetail> {
  return fetch(url, { credentials: 'include' }).then(r => json<SessionDetail>(r))
}

export function getSession(id: number): Promise<LiveSession> {
  return getSessionDetail(sessionUrl(id)).then(d => d.session)
}

export function createSession(title: string, opts: LiveOptions): Promise<{ session: LiveSession; heartbeat_interval: number }> {
  return fetch('/api/live/sessions', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, ...opts }),
  }).then(r => json<{ session: LiveSession; heartbeat_interval: number }>(r))
}

export interface HeartbeatStatus {
  battery_level?: number
  battery_charging?: boolean
  state?: string
}

export async function heartbeat(id: number, status: HeartbeatStatus = {}): Promise<void> {
  const res = await fetch(sessionUrl(id, '/heartbeat'), {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(status),
  })
  if (!res.ok) throw new HttpError(res.status)
}

export function createShareLink(id: number): Promise<{ token: string; path: string }> {
  return fetch(sessionUrl(id, '/share'), { method: 'POST', credentials: 'include' })
    .then(r => json<{ token: string; path: string }>(r))
}

export async function revokeShareLink(id: number): Promise<void> {
  const res = await fetch(sessionUrl(id, '/share'), { method: 'DELETE', credentials: 'include' })
  if (!res.ok) throw new HttpError(res.status)
}

export async function postTrack(id: number, points: TrackPoint[]): Promise<void> {
  const res = await fetch(sessionUrl(id, '/track'), {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ points }),
  })
  if (!res.ok) throw new HttpError(res.status)
}

export function getTrack(url: string, after = 0): Promise<TrackPoint[]> {
  return fetch(after ? `${url}?after=${after}` : url, { credentials: 'include' })
    .then(r => json<{ points: TrackPoint[] }>(r))
    .then(d => d.points)
}

export interface RecordingList {
  recordings: Recording[]
  used_bytes: number
  free_bytes?: number
}

export function listRecordings(): Promise<RecordingList> {
  return fetch('/api/live/recordings', { credentials: 'include' }).then(r => json<RecordingList>(r))
}

export function getRecording(id: number): Promise<Recording> {
  return fetch(`/api/live/recordings/${id}`, { credentials: 'include' })
    .then(r => json<{ recording: Recording }>(r))
    .then(d => d.recording)
}

export async function deleteRecording(id: number): Promise<void> {
  const res = await fetch(`/api/live/recordings/${id}`, { method: 'DELETE', credentials: 'include' })
  if (!res.ok) throw new HttpError(res.status)
}

export async function endSession(id: number): Promise<void> {
  const res = await fetch(sessionUrl(id, '/end'), { method: 'POST', credentials: 'include' })
  if (!res.ok && res.status !== 404) throw new HttpError(res.status)
}

export async function fetchIceServers(url = '/api/live/ice'): Promise<RTCIceServer[]> {
  try {
    const res = await fetch(url, { credentials: 'include' })
    if (!res.ok) return []
    const data = (await res.json()) as { iceServers?: RTCIceServer[] }
    return data.iceServers ?? []
  } catch {
    return []
  }
}

// ICE_GATHER_TIMEOUT_MS caps how long we wait for candidates before sending
// the offer. TURN allocation over a slow mobile link can take a while; after
// this we send whatever host/srflx/relay candidates we have.
const ICE_GATHER_TIMEOUT_MS = 3000

function waitForIceGathering(pc: RTCPeerConnection, timeoutMs = ICE_GATHER_TIMEOUT_MS): Promise<void> {
  if (pc.iceGatheringState === 'complete') return Promise.resolve()
  return new Promise(resolve => {
    const done = () => {
      clearTimeout(timer)
      pc.removeEventListener('icegatheringstatechange', onChange)
      resolve()
    }
    const onChange = () => {
      if (pc.iceGatheringState === 'complete') done()
    }
    const timer = setTimeout(done, timeoutMs)
    pc.addEventListener('icegatheringstatechange', onChange)
  })
}

/**
 * negotiate runs one WHIP/WHEP exchange against url and returns the session
 * resource URL (for DELETE on hang-up). Throws HttpError on a non-201.
 */
export async function negotiate(pc: RTCPeerConnection, url: string): Promise<string | null> {
  const offer = await pc.createOffer()
  await pc.setLocalDescription(offer)
  await waitForIceGathering(pc)
  const res = await fetch(url, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/sdp' },
    body: pc.localDescription?.sdp ?? offer.sdp,
  })
  if (res.status !== 201) throw new HttpError(res.status)
  const location = res.headers.get('Location')
  const answer = await res.text()
  await pc.setRemoteDescription({ type: 'answer', sdp: answer })
  return location
}

/** hangUp tells the media server to drop a WHIP/WHEP session. Best effort. */
export function hangUp(resource: string | null): void {
  if (!resource) return
  fetch(resource, { method: 'DELETE', credentials: 'include', keepalive: true }).catch(() => {})
}

/**
 * preferH264 moves H.264 to the front of a video transceiver's codec list.
 * MediaMTX can remux H.264 to HLS for every browser (Safari included), and
 * phones encode it in hardware, so it is the best default for both paths.
 */
export function preferH264(transceiver: RTCRtpTransceiver): void {
  const caps = typeof RTCRtpSender !== 'undefined' && RTCRtpSender.getCapabilities?.('video')
  if (!caps || typeof transceiver.setCodecPreferences !== 'function') return
  const h264 = caps.codecs.filter(c => c.mimeType.toLowerCase() === 'video/h264')
  if (h264.length === 0) return
  const rest = caps.codecs.filter(c => c.mimeType.toLowerCase() !== 'video/h264')
  try {
    transceiver.setCodecPreferences([...h264, ...rest])
  } catch {
    // Some browsers reject preference lists they consider invalid — keep defaults.
  }
}

export interface OutboundSample {
  /** Total bytes sent across all outbound RTP streams. */
  bytes: number
  /** Sample time in ms (performance.now()). */
  at: number
  /** The video encoder reports it is held back by available bandwidth. */
  bandwidthLimited: boolean
}

/** sampleOutbound sums bytesSent over every outbound-rtp stat. */
export async function sampleOutbound(pc: RTCPeerConnection): Promise<OutboundSample> {
  const stats = await pc.getStats()
  let bytes = 0
  let bandwidthLimited = false
  stats.forEach(report => {
    if (report.type === 'outbound-rtp' && typeof report.bytesSent === 'number') {
      bytes += report.bytesSent
      if (report.kind === 'video' && report.qualityLimitationReason === 'bandwidth') bandwidthLimited = true
    }
  })
  return { bytes, at: performance.now(), bandwidthLimited }
}

/** formatBytes renders a byte count as MB/GB for the data-used readout. */
export function formatBytes(bytes: number, locale: string): string {
  const nf = new Intl.NumberFormat(locale, { maximumFractionDigits: 1 })
  if (bytes >= 1e9) return `${nf.format(bytes / 1e9)} GB`
  return `${nf.format(bytes / 1e6)} MB`
}

/** formatDuration renders elapsed seconds as H:MM:SS / M:SS. */
export function formatDuration(totalSeconds: number): string {
  const s = Math.max(0, Math.floor(totalSeconds))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${h}:${pad(m)}:${pad(sec)}` : `${m}:${pad(sec)}`
}

// --- run stats ---------------------------------------------------------------

const EARTH_RADIUS_M = 6_371_000

/** haversine returns the great-circle distance between two fixes in metres. */
export function haversine(a: { lat: number; lon: number }, b: { lat: number; lon: number }): number {
  const rad = (d: number) => (d * Math.PI) / 180
  const dLat = rad(b.lat - a.lat)
  const dLon = rad(b.lon - a.lon)
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(rad(a.lat)) * Math.cos(rad(b.lat)) * Math.sin(dLon / 2) ** 2
  return 2 * EARTH_RADIUS_M * Math.asin(Math.sqrt(h))
}

// Fixes worse than this are ignored for distance (GPS drift while standing
// still or under trees would otherwise add phantom metres).
const MAX_ACCURACY_M = 35
// Current pace is measured over roughly the last minute of movement.
const PACE_WINDOW_S = 60

export interface RunStats {
  distanceM: number
  elapsedS: number
  /** Seconds per km over the whole run, or null before any distance. */
  avgPaceS: number | null
  /** Seconds per km over the last ~minute, or null when not moving. */
  currentPaceS: number | null
}

/** computeRunStats derives distance/pace from a GPS track (oldest first). */
export function computeRunStats(points: TrackPoint[]): RunStats {
  const good = points.filter(p => p.acc === undefined || p.acc <= MAX_ACCURACY_M)
  if (good.length < 2) return { distanceM: 0, elapsedS: 0, avgPaceS: null, currentPaceS: null }
  const times = good.map(p => Date.parse(p.t) / 1000)
  let distance = 0
  const cumulative = [0]
  for (let i = 1; i < good.length; i++) {
    distance += haversine(good[i - 1], good[i])
    cumulative.push(distance)
  }
  const elapsed = times[times.length - 1] - times[0]
  const lastT = times[times.length - 1]
  let j = good.length - 1
  while (j > 0 && lastT - times[j - 1] <= PACE_WINDOW_S) j--
  const windowD = distance - cumulative[j]
  const windowT = lastT - times[j]
  const pace = (d: number, t: number) => (d > 5 && t > 0 ? t / (d / 1000) : null)
  const current = windowT >= 15 ? pace(windowD, windowT) : null
  return {
    distanceM: distance,
    elapsedS: elapsed,
    avgPaceS: pace(distance, elapsed),
    // Slower than 20:00/km is walking or standing — show no pace.
    currentPaceS: current !== null && current < 1200 ? current : null,
  }
}

/** formatPace renders seconds-per-km as M:SS. */
export function formatPace(secPerKm: number | null): string {
  if (secPerKm === null || !Number.isFinite(secPerKm)) return '–'
  const s = Math.round(secPerKm)
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}

/** formatKm renders metres as km with two decimals in the given locale. */
export function formatKm(meters: number, locale: string): string {
  return new Intl.NumberFormat(locale, { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(meters / 1000)
}
