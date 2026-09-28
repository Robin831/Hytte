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
  `cmd/server/main.go` ends any session with no heartbeat for 5 minutes (dead
  battery, closed tab, long dead zone) and kicks its MediaMTX publisher and
  viewers. Going live again ends the user's previous session. Nothing is
  recorded.

## Go live options and features

On the Go live screen the broadcaster picks, per broadcast:

| Option | Default | What it does |
|---|---|---|
| **Notify family** | on | Push "<name> is live" to every other user with the `livestream` feature (admins included), skipping anyone in quiet hours. Tapping it opens `/live/{id}`. |
| **Record** | off | Records the broadcast and builds a replay afterwards (see *Recording*). Greyed out when the server can't record; refused with 507 when free disk space is below `LIVE_RECORDING_MIN_FREE_MB` (default 2 GB). |
| **Share location** | last choice | The phone sends GPS fixes (batched every 5 s). Viewers get a live route map (Leaflet + OpenStreetMap tiles) with distance, current pace, average pace and time. Each point is encrypted at rest. Without recording, the track is purged 30 minutes after the stream ends; with recording it's kept for the replay. |

While live:

- **Your live link** (on the Go live screen, before and during a broadcast): a permanent personal `/watch/<token>` URL. Send it once, ahead of the run, so you never have to switch apps mid-stream (phones suspend the page when you do). It always shows whatever you're streaming right now. While you're offline it says "<name> isn't live right now" and starts playing by itself when you go live, including when you continue as a new broadcast. **Reset** issues a new URL and kills the old one. The table is `live_user_links`: SHA-256 hash for lookup, token encrypted so you can copy it again. Viewers only see your first name. (The older per-session link API, `POST /api/live/sessions/{id}/share`, still works but has no button any more.)
- **Leaving the page never ends the broadcast.** Only Stop does. Phones fire `pagehide` when they suspend or reload a background tab, and ending the stream there is exactly what broke streams when sharing from another app. What happens instead:
  - On reload or restore, the page finds the session it was sending to (remembered on the device) and **resumes it**: same session, same links, recording continues.
  - On returning from the background, it reopens the camera or mic if the phone ended them, heartbeats immediately, and reconnects without waiting out the backoff.
  - If the phone was away longer than the 5-minute stale timeout, the reaper has ended the session. The page then **continues as a new broadcast** with the same settings and no second notification. Member viewers on `/live/{id}` follow automatically, and personal-link viewers just keep watching.
  - A live session started in another tab or on another device is offered as "Continue here".
- **Battery and connection state** go out with every heartbeat. Viewers see a battery badge (red below 20%). While the phone is reconnecting, or the video stalls (no decoded frames for 4 s), viewers see "Back in a moment…" over the last frame instead of a black box.
- **Auto low-data**: after a 20 s warm-up, if the encoder reports it is bandwidth-limited and the send rate stays under 500 kbps for ~10 s, the phone switches to low-data mode (480p, 600 kbps) and says so.

### Recording

1. At go-live Hytte adds an exact-name MediaMTX path config (`POST /v3/config/paths/add/live/<key>` with `{"record":true}`). It takes precedence over the `~^live/…` regex, whose default is `record: false`.
2. MediaMTX writes fMP4 segments to `/var/lib/mediamtx/recordings/live/<key>/`.
3. When the session ends (Stop, reaper, or going live again), Hytte removes the path config, waits 3 s for the last segment to flush, then runs **one ffmpeg at a time**: concat demuxer, video copied, Opus converted to AAC (Safari/iOS can't play Opus in MP4), `+faststart`. The result is `/var/lib/hytte-live/session-<id>-<rand>.mp4` (`LIVE_RECORDINGS_DIR`).
4. The replay is linked to the owner's workout that overlaps the broadcast most (±15 min). This happens when the replay is built and again when it's viewed, because the watch usually syncs after the run.
5. Replays interrupted by a Hytte restart are finished on startup (`ResumePending`).

MediaMTX creates each stream's segment folder without group write permission, so Hytte can read segments but not delete them. MediaMTX's `recordDeleteAfter: 1d` cleans them up instead.

Replays live under **Live → Replays** (all feature users can watch; only the owner can delete) at `/live/replay/{id}`. The video is served with range support (seekable). When there's a GPS track, a marker on the map follows the video position.

## API

All routes require a session cookie and the `livestream` feature (admins bypass it).

| Method | Path | Who | Purpose |
|---|---|---|---|
| GET | /api/live/ice | any | STUN/TURN servers for `RTCPeerConnection` |
| GET | /api/live/sessions | any | Live sessions (`on_air`, `viewers`), plus `configured` |
| POST | /api/live/sessions | any | Go live: `{title, notify, record, location}` → `{session, heartbeat_interval}` |
| GET | /api/live/sessions/{id} | any | One session, including ended ones |
| POST | /api/live/sessions/{id}/heartbeat | owner | Keep alive with `{state, battery_level?, battery_charging?}`; returns 409 once ended |
| POST | /api/live/sessions/{id}/end | owner | End and kick everyone |
| POST | /api/live/sessions/{id}/whip | owner | WHIP offer → answer (201 + `Location`) |
| PATCH/DELETE | /api/live/sessions/{id}/whip/{resource} | owner | Trickle ICE / hang up |
| POST | /api/live/sessions/{id}/whep | any | WHEP offer → answer |
| PATCH/DELETE | /api/live/sessions/{id}/whep/{resource} | any | Trickle ICE / hang up |
| GET | /api/live/sessions/{id}/hls/{file} | any | HLS playlists/segments, sent with `Cache-Control: private, no-store` |
| POST/DELETE | /api/live/sessions/{id}/share | owner | Create (or rotate) / revoke the share link → `{token, path}` |
| GET/POST/DELETE | /api/live/my-link | any | Personal live link: get `{path}` (null if none) / create or reset / turn off |
| POST | /api/live/sessions/{id}/track | owner | Upload GPS fixes `{points:[{t,lat,lon,alt?,acc?}]}` (≤200 per request; needs Share location) |
| GET | /api/live/sessions/{id}/track?after={pointId} | any | GPS track, incrementally |
| GET | /api/live/recordings | any | Replays plus `used_bytes` / `free_bytes` |
| GET | /api/live/recordings/{id} | any | One replay (links the workout lazily) |
| GET | /api/live/recordings/{id}/video | any | The MP4 (HTTP range requests) |
| GET | /api/live/recordings/{id}/track | any | The route kept with the replay |
| DELETE | /api/live/recordings/{id} | owner | Delete the replay, its file and its track |

Share-link routes have **no session auth**; the 64-hex token in the path is the credential:

| Method | Path | Purpose |
|---|---|---|
| GET | /api/live/public/{token} | Session status (trimmed: first name only, no user id). For a personal link whose owner is offline: `{"session": null, "owner_name": "…"}` |
| GET | /api/live/public/{token}/ice | TURN credentials, only while live |
| POST, PATCH/DELETE | /api/live/public/{token}/whep[/{resource}] | Watch (WHEP). WHIP is never reachable this way |
| GET | /api/live/public/{token}/hls/{file} | HLS fallback |
| GET | /api/live/public/{token}/track | GPS track while live |

For a session that has ended, new WHIP/WHEP/HLS requests get 410. DELETE still
works so clients can clean up.

## Server setup (production)

### MediaMTX

- Binary: `/usr/local/bin/mediamtx` (release tarball from
  github.com/bluenviron/mediamtx, checksum-verified; v1.21.1 at install).
- Config: `/etc/mediamtx/mediamtx.yml`, `root:mediamtx 0640` (it holds secrets).
- Service: `mediamtx.service`, runs as the `mediamtx` system user with
  `ProtectSystem=strict` / `ProtectHome=true`, `ReadWritePaths=/var/lib/mediamtx`
  and `UMask=0007`.
- Folders: `/var/lib/mediamtx/recordings` (`mediamtx:robin 2770`, so Hytte can
  read segments) and `/var/lib/hytte-live` (`robin:robin 0750`, finished replays).
  `ffmpeg`/`ffprobe` come from the Ubuntu package.

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
  record: false             # Hytte turns it on per broadcast via the API
  recordPath: /var/lib/mediamtx/recordings/%path/%Y-%m-%d_%H-%M-%S-%f
  recordFormat: fmp4
  recordSegmentDuration: 1h
  recordDeleteAfter: 1d     # Hytte can't delete segments itself (folder perms)

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
| `LIVE_RECORDING_SEGMENTS_DIR` | `/var/lib/mediamtx/recordings` (MediaMTX's record root) |
| `LIVE_RECORDINGS_DIR` | `/var/lib/hytte-live` (finished replays) |
| `LIVE_RECORDING_MIN_FREE_MB` | optional, default `2048` |

Recording is off (the checkbox is greyed out) unless both folders are set,
ffmpeg is on `PATH`, and the MediaMTX API is configured.

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
- Disk: the server has ~11 GB free as of Sept 2026, which is roughly 18 hours of
  replays. The Replays header shows used/free space; delete old replays there.
  `journalctl -u hytte | grep 'livestream: replay'` shows replay builds.

## Later

A "cheer" button for viewers (sound or text-to-speech on the phone), and
permanent UniFi Protect cameras (RTSP over a WireGuard tunnel from the
UCG‑Fiber, pulled into the same MediaMTX and `/live` viewer).
