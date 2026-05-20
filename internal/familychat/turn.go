package familychat

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

// WebRTCConfig holds the resolved STUN/TURN servers and optional coturn
// shared-secret used to mint short-lived ephemeral credentials. Built once
// per request from environment variables; nothing is persisted.
//
// The config layer keys (when an operator chooses to mirror them into a
// future YAML/TOML layer) are:
//
//	webrtc.stun_urls         -> WEBRTC_STUN_URLS         (comma-separated)
//	webrtc.turn_urls         -> WEBRTC_TURN_URLS         (comma-separated)
//	webrtc.turn_username     -> WEBRTC_TURN_USERNAME     (static credential)
//	webrtc.turn_credential   -> WEBRTC_TURN_CREDENTIAL   (static credential)
//	webrtc.turn_shared_secret-> WEBRTC_TURN_SHARED_SECRET(coturn REST API)
//	webrtc.turn_ttl_seconds  -> WEBRTC_TURN_TTL_SECONDS  (ephemeral lifetime)
type WebRTCConfig struct {
	STUNURLs       []string
	TURNURLs       []string
	TURNUsername   string
	TURNCredential string
	SharedSecret   string
	TTL            time.Duration
}

const (
	// defaultTURNTTL bounds the lifetime of ephemeral credentials minted via
	// the coturn REST API. One hour matches coturn's documented default and
	// is short enough to limit reuse if a peer leaks its credential.
	defaultTURNTTL = 1 * time.Hour
	// minTURNTTL guards against pathologically short TTLs that would
	// invalidate the credential before a call could even establish.
	minTURNTTL = 1 * time.Minute
)

// LoadWebRTCConfig reads the webrtc.* configuration from the environment.
// Missing keys are treated as zero values — the handler degrades gracefully
// to STUN-only (or an empty list) rather than erroring, so a partial
// rollout does not break the call UI for users on the older config.
func LoadWebRTCConfig() WebRTCConfig {
	return WebRTCConfig{
		STUNURLs:       splitURLs(os.Getenv("WEBRTC_STUN_URLS")),
		TURNURLs:       splitURLs(os.Getenv("WEBRTC_TURN_URLS")),
		TURNUsername:   strings.TrimSpace(os.Getenv("WEBRTC_TURN_USERNAME")),
		TURNCredential: strings.TrimSpace(os.Getenv("WEBRTC_TURN_CREDENTIAL")),
		SharedSecret:   strings.TrimSpace(os.Getenv("WEBRTC_TURN_SHARED_SECRET")),
		TTL:            ttlFromEnv(os.Getenv("WEBRTC_TURN_TTL_SECONDS")),
	}
}

func splitURLs(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func ttlFromEnv(s string) time.Duration {
	if strings.TrimSpace(s) == "" {
		return defaultTURNTTL
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return defaultTURNTTL
	}
	d := time.Duration(n) * time.Second
	if d < minTURNTTL {
		return minTURNTTL
	}
	return d
}

// ICEServer mirrors the WebRTC RTCIceServer dictionary so the frontend can
// pass the JSON straight into new RTCPeerConnection({iceServers: [...]})
// without renaming fields.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// ICEConfig is the wire shape returned by GET /api/familychat/turn.
type ICEConfig struct {
	ICEServers []ICEServer `json:"iceServers"`
	// TTL is the number of seconds the TURN credential is valid for. Clients
	// should refetch the config and renegotiate the peer connection before
	// this expires for calls that outlive the TTL. Zero when no TURN
	// credential was issued (STUN-only or no auth).
	TTL int `json:"ttl,omitempty"`
}

// BuildICEConfig assembles the ICE server list at the supplied issue time.
// The clock is dependency-injected so tests can pin the expiry timestamp
// for deterministic HMAC assertions.
//
// Precedence: SharedSecret > static TURNUsername/TURNCredential > anonymous
// TURN (URLs only). This mirrors coturn's recommended layering — once a
// shared secret is in place every static credential becomes a fallback
// only used while migrating between deployments.
func BuildICEConfig(cfg WebRTCConfig, username string, now time.Time) ICEConfig {
	out := ICEConfig{}

	if len(cfg.STUNURLs) > 0 {
		out.ICEServers = append(out.ICEServers, ICEServer{URLs: cfg.STUNURLs})
	}

	if len(cfg.TURNURLs) == 0 {
		return out
	}

	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = defaultTURNTTL
	}

	switch {
	case cfg.SharedSecret != "":
		// coturn REST API spec (draft-uberti-behave-turn-rest-00):
		//   username   = "<unix-expiry-seconds>:<identity>"
		//   credential = base64(HMAC-SHA1(secret, username))
		// The identity portion lets an operator correlate TURN sessions
		// with Hytte users in the coturn log; it is not strictly required
		// by the spec, but a leading timestamp on its own breaks coturn's
		// expectation of a colon-delimited username.
		identity := username
		if identity == "" {
			identity = "hytte"
		}
		expiry := now.Add(ttl).Unix()
		turnUser := fmt.Sprintf("%d:%s", expiry, identity)
		credential := signCoturnCredential(cfg.SharedSecret, turnUser)
		out.ICEServers = append(out.ICEServers, ICEServer{
			URLs:       cfg.TURNURLs,
			Username:   turnUser,
			Credential: credential,
		})
		out.TTL = int(ttl.Seconds())
	case cfg.TURNUsername != "" && cfg.TURNCredential != "":
		out.ICEServers = append(out.ICEServers, ICEServer{
			URLs:       cfg.TURNURLs,
			Username:   cfg.TURNUsername,
			Credential: cfg.TURNCredential,
		})
	default:
		// TURN URLs configured but no credentials — return them anyway so a
		// deliberately anonymous TURN deployment still works. coturn refuses
		// these by default, but operators sometimes run unauthenticated
		// turnservers on private networks.
		out.ICEServers = append(out.ICEServers, ICEServer{URLs: cfg.TURNURLs})
	}
	return out
}

// signCoturnCredential produces the base64-encoded HMAC-SHA1 of username
// keyed by sharedSecret, matching the credential coturn computes when the
// `use-auth-secret` mode is enabled. Exported via BuildICEConfig only —
// tests reach in via that path so this stays an implementation detail.
func signCoturnCredential(sharedSecret, username string) string {
	mac := hmac.New(sha1.New, []byte(sharedSecret))
	mac.Write([]byte(username))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// TurnConfigHandler returns the current STUN/TURN configuration so the
// frontend can pass it straight into new RTCPeerConnection. Session-auth
// gated by the surrounding router group — the handler itself only reads
// the user id (to scope the ephemeral coturn identity) and never falls
// back to anonymous behaviour.
func TurnConfigHandler() http.HandlerFunc {
	return turnConfigHandler(LoadWebRTCConfig, time.Now)
}

// turnConfigHandler is the testable inner constructor. loader and clock are
// injected so tests can pin both the environment and the expiry timestamp.
func turnConfigHandler(loader func() WebRTCConfig, clock func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var identity string
		if user != nil && user.ID > 0 {
			identity = strconv.FormatInt(user.ID, 10)
		}
		cfg := loader()
		ice := BuildICEConfig(cfg, identity, clock())
		writeJSON(w, http.StatusOK, ice)
	}
}
