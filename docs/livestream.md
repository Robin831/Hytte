# Livestream — phone broadcasting (Hytte-krwxy)

Stream live video from a phone browser into Hytte, and watch it live from any
other signed-in account that has the `livestream` feature. The main use is live
runs, so the phone publishes straight to the server over mobile data. Nothing
is needed on the home network.

```
Phone (Go live page, getUserMedia)
  ── WHIP: POST SDP ──▶ Hytte /api/live/sessions/{id}/whip   (RequireAuth + feature)
                        └─ reverse proxy ─▶ MediaMTX 127.0.0.1:8889
  ◀═ WebRTC media, UDP/TCP 8189 direct to the server (or relayed via coturn) ═▶

Viewer (Live page)
  ── WHEP: POST SDP ──▶ Hytte /api/live/sessions/{id}/whep ─▶ MediaMTX :8889
  └─ fallback: HLS ──▶ Hytte /api/live/sessions/{id}/hls/* ─▶ MediaMTX 127.0.0.1:8888
```

- **MediaMTX** does the media work: WHIP ingest, WHEP/WebRTC fan-out, and
  low-latency HLS for viewers whose network blocks WebRTC. All of its HTTP
  listeners are bound to localhost. The only public port is WebRTC media on
  8189 (UDP, plus TCP as a fallback).
- **Hytte handles all auth.** Every signalling and HLS request goes through
  Hytte, where `RequireAuth` and the `livestream` feature gate apply. Hytte
  then calls MediaMTX with its own credentials: basic-auth users for WHIP and
  WHEP, and the `hlsCDNSecret` Bearer token for HLS. Clients only ever see
  numeric session ids. The MediaMTX path (`live/<128-bit key>`) never leaves
  the server, and the `Location` headers are rewritten to Hytte URLs.
- **ICE/TURN** reuses the Family Chat config (`WEBRTC_*` env vars, coturn with
  ephemeral credentials, see `docs/familychat-turn.md`) through
  `GET /api/live/ice`. Mobile carrier NATs often need that TURN relay.
- **Session lifecycle:** "Go live" creates a `live_sessions` row (the title is
  encrypted). The broadcaster sends a heartbeat every 15 s. A reaper in
  `cmd/server/main.go` ends any session with no heartbeat for 2 minutes (dead
  battery, closed tab, long dead zone) and kicks its MediaMTX publisher and
  viewers. Going live again ends the user's previous session. Nothing is
  recorded.

## API

All routes require a session cookie and the `livestream` feature (admins bypass it).

| Method | Path | Who | Purpose |
|---|---|---|---|
| GET | /api/live/ice | any | STUN/TURN servers for `RTCPeerConnection` |
| GET | /api/live/sessions | any | Live sessions (`on_air`, `viewers`), plus `configured` |
| POST | /api/live/sessions | any | Go live: `{title}` → `{session, heartbeat_interval}` |
| GET | /api/live/sessions/{id} | any | One session, including ended ones |
| POST | /api/live/sessions/{id}/heartbeat | owner | Keep alive; returns 409 once ended |
| POST | /api/live/sessions/{id}/end | owner | End and kick everyone |
| POST | /api/live/sessions/{id}/whip | owner | WHIP offer → answer (201 + `Location`) |
| PATCH/DELETE | /api/live/sessions/{id}/whip/{resource} | owner | Trickle ICE / hang up |
| POST | /api/live/sessions/{id}/whep | any | WHEP offer → answer |
| PATCH/DELETE | /api/live/sessions/{id}/whep/{resource} | any | Trickle ICE / hang up |
| GET | /api/live/sessions/{id}/hls/{file} | any | HLS playlists/segments, sent with `Cache-Control: private, no-store` |

For a session that has ended, new WHIP/WHEP/HLS requests get 410. DELETE still
works so clients can clean up.

## Server setup (production)

### MediaMTX

- Binary: `/usr/local/bin/mediamtx` (release tarball from
  github.com/bluenviron/mediamtx, checksum-verified; v1.21.1 at install).
- Config: `/etc/mediamtx/mediamtx.yml`, `root:mediamtx 0640` (it holds secrets).
- Service: `mediamtx.service`, runs as the `mediamtx` system user with
  `ProtectSystem=strict` / `ProtectHome=true`.

```yaml
authMethod: internal
authInternalUsers:
  - user: hytte-publisher
    pass: <LIVE_PUBLISH_PASS>
    ips: ["127.0.0.1", "::1"]
    permissions: [{action: publish, path: "~^live/[0-9a-f]{32}$"}]
  - user: hytte-reader
    pass: <LIVE_READ_PASS>
    ips: ["127.0.0.1", "::1"]
    permissions: [{action: read, path: "~^live/[0-9a-f]{32}$"}]
  - user: hytte-api
    pass: <LIVE_API_PASS>
    ips: ["127.0.0.1", "::1"]
    permissions: [{action: api}]

api: true
apiAddress: 127.0.0.1:9997
metrics: false
pprof: false
playback: false
rtsp: false
rtmp: false
srt: false
moq: false

hls: true
hlsAddress: 127.0.0.1:8888
hlsVariant: lowLatency
hlsCDNSecret: "<LIVE_HLS_SECRET>"   # CDN mode: shared muxer, no per-viewer cookies

webrtc: true
webrtcAddress: 127.0.0.1:8889
webrtcLocalUDPAddress: :8189
webrtcLocalTCPAddress: :8189
webrtcIPsFromInterfacesList: [eth0]
webrtcAdditionalHosts: [<server public IPv4>]  # the domain resolves to Cloudflare, not us

pathDefaults:
  source: publisher
  overridePublisher: true   # a reconnecting phone replaces its own stale publisher
  record: false

paths:
  "~^live/[0-9a-f]{32}$": {}
```

Why CDN mode for HLS: since v1.21, MediaMTX tracks each HLS viewer with a
cookie and answers the first playlist request with a `302 ?cookieCheck=1`.
That doesn't work through a proxy. With `hlsCDNSecret` set, requests that carry
`Authorization: Bearer <secret>` share one muxer session and skip MediaMTX
auth. That is safe here because the HLS listener is localhost-only and Hytte
authorises every viewer.

### Firewall

```bash
sudo ufw allow 8189/udp comment 'MediaMTX WebRTC media (Hytte livestream)'
sudo ufw allow 8189/tcp comment 'MediaMTX WebRTC ICE-TCP fallback'
```

coturn already allows relaying to the server's public IP, since only private
and loopback ranges are in `denied-peer-ip`.

### Hytte environment (`~/Hytte/.env`)

| Var | Example |
|---|---|
| `LIVE_MEDIAMTX_WEBRTC_URL` | `http://127.0.0.1:8889` |
| `LIVE_MEDIAMTX_HLS_URL` | `http://127.0.0.1:8888` |
| `LIVE_MEDIAMTX_API_URL` | `http://127.0.0.1:9997` |
| `LIVE_HLS_SECRET` | same value as `hlsCDNSecret` |
| `LIVE_PUBLISH_USER` / `LIVE_PUBLISH_PASS` | `hytte-publisher` / … |
| `LIVE_READ_USER` / `LIVE_READ_PASS` | `hytte-reader` / … |
| `LIVE_API_USER` / `LIVE_API_PASS` | `hytte-api` / … |

If `LIVE_MEDIAMTX_WEBRTC_URL` is unset, the Live page says livestreaming isn't
configured and `POST /api/live/sessions` returns 503.

## Operating notes

- `journalctl -u mediamtx -f` shows publish/read sessions and the ICE candidate
  pair in use.
- `curl -u hytte-api:$LIVE_API_PASS http://127.0.0.1:9997/v3/paths/list`
  lists active streams.
- Phones pause the camera when the screen locks or the browser is
  backgrounded. The page holds a Screen Wake Lock while live, but the phone
  has to stay unlocked in a mount.
- Data use: normal quality (720p, 1.5 Mbps cap) is about 0.5–0.7 GB/hour.
  Low-data mode (480p, 600 kbps) is about 0.3 GB/hour.

## Later

Live GPS map next to the video, a "… is live" push notification, recording and
replay, share links for people without an account, and permanent UniFi Protect
cameras (RTSP over a WireGuard tunnel from the UCG‑Fiber, pulled into the same
MediaMTX and `/live` viewer).
