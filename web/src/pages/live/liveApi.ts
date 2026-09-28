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
}

export interface LiveList {
  sessions: LiveSession[]
  configured: boolean
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

export function getSession(id: number): Promise<LiveSession> {
  return fetch(sessionUrl(id), { credentials: 'include' })
    .then(r => json<{ session: LiveSession }>(r))
    .then(d => d.session)
}

export function createSession(title: string): Promise<{ session: LiveSession; heartbeat_interval: number }> {
  return fetch('/api/live/sessions', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title }),
  }).then(r => json<{ session: LiveSession; heartbeat_interval: number }>(r))
}

export async function heartbeat(id: number): Promise<void> {
  const res = await fetch(sessionUrl(id, '/heartbeat'), { method: 'POST', credentials: 'include' })
  if (!res.ok) throw new HttpError(res.status)
}

export async function endSession(id: number): Promise<void> {
  const res = await fetch(sessionUrl(id, '/end'), { method: 'POST', credentials: 'include' })
  if (!res.ok && res.status !== 404) throw new HttpError(res.status)
}

export async function fetchIceServers(): Promise<RTCIceServer[]> {
  try {
    const res = await fetch('/api/live/ice', { credentials: 'include' })
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
}

/** sampleOutbound sums bytesSent over every outbound-rtp stat. */
export async function sampleOutbound(pc: RTCPeerConnection): Promise<OutboundSample> {
  const stats = await pc.getStats()
  let bytes = 0
  stats.forEach(report => {
    if (report.type === 'outbound-rtp' && typeof report.bytesSent === 'number') {
      bytes += report.bytesSent
    }
  })
  return { bytes, at: performance.now() }
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
