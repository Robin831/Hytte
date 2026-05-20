# Family Chat — STUN / TURN configuration

Family Chat voice and video calls use WebRTC. WebRTC needs at minimum a
STUN server to discover each peer's public address, and a TURN relay for
calls between peers that cannot punch a direct path through NAT
(symmetric NATs, strict firewalls, mobile carriers). This document
describes how Hytte resolves the ICE server list and how to deploy a
coturn TURN relay to back the production server.

## Endpoint

```
GET /api/familychat/turn
```

* Session-auth gated. Anonymous callers get the standard `401`.
* Response is a single `RTCConfiguration`-shaped JSON object that the
  frontend can pass straight into `new RTCPeerConnection({ iceServers })`.

Example response (coturn REST API mode):

```json
{
  "iceServers": [
    { "urls": ["stun:turn.example.com:3478"] },
    {
      "urls": ["turn:turn.example.com:3478", "turns:turn.example.com:5349"],
      "username": "1748563200:42",
      "credential": "iC6FBpa7zUmW2X4j3pYTH+0FPgI="
    }
  ],
  "ttl": 3600
}
```

`ttl` is the number of seconds the TURN credential is valid for. The
frontend should refetch and renegotiate before this expires for calls
longer than the TTL.

## Configuration keys

Hytte reads the WebRTC config from the process environment. The mapping
of logical key → env var is:

| Logical key                  | Env var                       | Purpose                                                       |
| ---------------------------- | ----------------------------- | ------------------------------------------------------------- |
| `webrtc.stun_urls`           | `WEBRTC_STUN_URLS`            | Comma-separated STUN URLs (e.g. `stun:stun.l.google.com:19302`). |
| `webrtc.turn_urls`           | `WEBRTC_TURN_URLS`            | Comma-separated TURN URLs (`turn:` or `turns:`).              |
| `webrtc.turn_username`       | `WEBRTC_TURN_USERNAME`        | Static TURN username (fallback when no shared secret is set). |
| `webrtc.turn_credential`     | `WEBRTC_TURN_CREDENTIAL`      | Static TURN credential paired with the username above.        |
| `webrtc.turn_shared_secret`  | `WEBRTC_TURN_SHARED_SECRET`   | coturn `use-auth-secret` shared secret. Enables ephemeral creds. |
| `webrtc.turn_ttl_seconds`    | `WEBRTC_TURN_TTL_SECONDS`     | TTL for ephemeral credentials. Defaults to 3600 (1h), floored at 60. |

**Precedence**: `WEBRTC_TURN_SHARED_SECRET` wins over the static
`WEBRTC_TURN_USERNAME` / `WEBRTC_TURN_CREDENTIAL` pair. If only TURN URLs
are configured (no creds, no secret), the endpoint returns the URLs with
no credentials — coturn refuses these by default, but operators
sometimes run unauthenticated turnservers on private networks.

If nothing is configured, the endpoint returns `{"iceServers": []}` and
the call UI falls back to direct connections, which works for peers on
the same LAN.

## coturn deploy

Production hosts coturn on `turn.robinedvardsmith.com:3478` (and
`:5349` for TLS). The recommended setup uses the REST API shared-secret
mode so credentials never persist on disk in a database.

### 1. Install

On Debian/Ubuntu:

```bash
sudo apt-get install -y coturn
```

Enable the service:

```bash
sudo sed -i 's/^#TURNSERVER_ENABLED=1/TURNSERVER_ENABLED=1/' /etc/default/coturn
```

### 2. Generate a shared secret

```bash
openssl rand -hex 32
```

Store the value in:

* `/etc/turnserver.conf` (on the coturn host) as the value of
  `static-auth-secret=`.
* Hytte's `.env` as `WEBRTC_TURN_SHARED_SECRET=...`.

Both must match exactly. Rotating the secret invalidates every
in-flight credential immediately — coordinate with active call windows.

### 3. `/etc/turnserver.conf`

Minimum viable config:

```ini
listening-port=3478
tls-listening-port=5349
fingerprint
use-auth-secret
static-auth-secret=<the-32-byte-secret>
realm=robinedvardsmith.com
total-quota=100
bps-capacity=0
stale-nonce
no-multicast-peers
cert=/etc/letsencrypt/live/turn.robinedvardsmith.com/fullchain.pem
pkey=/etc/letsencrypt/live/turn.robinedvardsmith.com/privkey.pem
```

Pull the TLS cert via certbot:

```bash
sudo certbot certonly --standalone -d turn.robinedvardsmith.com
```

Open the relay port range you allow (defaults to 49152–65535) at the
firewall, plus 3478/udp+tcp and 5349/tcp.

### 4. Hytte env

Append to `~/Hytte/.env`:

```bash
WEBRTC_STUN_URLS=stun:turn.robinedvardsmith.com:3478
WEBRTC_TURN_URLS=turn:turn.robinedvardsmith.com:3478,turns:turn.robinedvardsmith.com:5349
WEBRTC_TURN_SHARED_SECRET=<paste the secret you generated>
WEBRTC_TURN_TTL_SECONDS=3600
```

Restart the Hytte service:

```bash
sudo systemctl restart hytte
```

### 5. Verify

```bash
curl -s --cookie 'session=...' https://robinedvardsmith.com/api/familychat/turn | jq
```

You should see an `iceServers` array with two entries and a `ttl` of
`3600`. The TURN `username` will be `<unix-timestamp>:<user_id>` — the
timestamp is roughly `(now + 3600)` in seconds.

Test the relay itself with the
[Trickle ICE](https://webrtc.github.io/samples/src/content/peerconnection/trickle-ice/)
sample page. Paste the TURN URL plus the username/credential pair from
the API response. You should see at least one `relay` candidate in the
results — that confirms coturn is actually relaying media.

## Credential format (coturn REST API)

The shared-secret flow follows
`draft-uberti-behave-turn-rest-00`:

```
username   = "<unix-expiry-seconds>:<identity>"
credential = base64(HMAC-SHA1(static-auth-secret, username))
```

* `unix-expiry-seconds` is `now + WEBRTC_TURN_TTL_SECONDS` at the moment
  the Hytte handler builds the response.
* `identity` is the authenticated user's id (or `hytte` for
  internal/test callers). It lets you correlate TURN sessions with
  application users in the coturn log without exposing user ids over
  the wire.

`internal/familychat/turn.go` implements both halves; the unit tests in
`turn_test.go` pin the format against a known HMAC vector so the
specification can't drift silently.

## Troubleshooting

* **Call UI says "connecting…" forever** — the client almost certainly
  never reached coturn. Check the browser console for ICE failures, then
  check that `iceServers` in the `/api/familychat/turn` response is
  non-empty and reachable from the client's network.
* **Credentials rejected (401 in coturn log)** — usually a secret
  mismatch between Hytte and `/etc/turnserver.conf`, or a clock skew
  greater than the TTL.
* **No `relay` candidates in trickle-ice** — check the coturn firewall
  rules (the relay port range needs to be open) and that
  `external-ip=` is set when coturn is behind NAT.
