package livestream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

// maxSDPBytes bounds WHIP/WHEP offer and trickle-ICE bodies. Real offers are a
// few KB; this stops a client streaming an unbounded body through the proxy.
const maxSDPBytes = 64 << 10

// Config holds the MediaMTX endpoints and the internal-auth users Hytte uses
// when proxying. All URLs point at localhost listeners; MediaMTX is never
// reachable from the internet except for WebRTC media on UDP/TCP 8189.
type Config struct {
	WebRTCURL string // e.g. http://127.0.0.1:8889 (WHIP/WHEP)
	HLSURL    string // e.g. http://127.0.0.1:8888
	// HLSSecret is MediaMTX's hlsCDNSecret. Hytte presents it as a Bearer
	// token so MediaMTX treats the proxy as a CDN: one shared muxer session,
	// no per-viewer cookie dance. Viewer auth happens in Hytte.
	HLSSecret   string
	APIURL      string // e.g. http://127.0.0.1:9997
	PublishUser string
	PublishPass string
	ReadUser    string
	ReadPass    string
	APIUser     string
	APIPass     string
}

// ConfigFromEnv reads the LIVE_* environment variables.
func ConfigFromEnv() Config {
	get := func(k string) string { return strings.TrimSpace(os.Getenv(k)) }
	return Config{
		WebRTCURL:   strings.TrimRight(get("LIVE_MEDIAMTX_WEBRTC_URL"), "/"),
		HLSURL:      strings.TrimRight(get("LIVE_MEDIAMTX_HLS_URL"), "/"),
		HLSSecret:   get("LIVE_HLS_SECRET"),
		APIURL:      strings.TrimRight(get("LIVE_MEDIAMTX_API_URL"), "/"),
		PublishUser: get("LIVE_PUBLISH_USER"),
		PublishPass: get("LIVE_PUBLISH_PASS"),
		ReadUser:    get("LIVE_READ_USER"),
		ReadPass:    get("LIVE_READ_PASS"),
		APIUser:     get("LIVE_API_USER"),
		APIPass:     get("LIVE_API_PASS"),
	}
}

// Enabled reports whether a media server is configured at all.
func (c Config) Enabled() bool {
	return c.WebRTCURL != ""
}

// MediaServer talks to MediaMTX on behalf of the handlers.
type MediaServer struct {
	cfg    Config
	client *http.Client
}

// NewMediaServer builds a MediaServer. The HTTP client has no overall timeout
// because HLS low-latency playlist requests legitimately block for a few
// seconds; callers bound requests with the incoming request's context.
func NewMediaServer(cfg Config) *MediaServer {
	return &MediaServer{cfg: cfg, client: &http.Client{
		// Never follow redirects: a Location from MediaMTX is rewritten, not chased.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// signalKind selects WHIP (publish) or WHEP (read).
type signalKind string

const (
	kindWHIP signalKind = "whip"
	kindWHEP signalKind = "whep"
)

func (m *MediaServer) credsFor(kind signalKind) (string, string) {
	if kind == kindWHIP {
		return m.cfg.PublishUser, m.cfg.PublishPass
	}
	return m.cfg.ReadUser, m.cfg.ReadPass
}

// proxySignal forwards a WHIP/WHEP request for the given MediaMTX path.
// resource is "" for the initial POST and the MediaMTX session id for
// PATCH (trickle ICE) / DELETE (hang up). locationPrefix is the Hytte URL
// prefix the resource id is appended to when rewriting Location, so clients
// only ever see Hytte URLs.
func (m *MediaServer) proxySignal(w http.ResponseWriter, r *http.Request, kind signalKind, mediaPath, resource, locationPrefix string) {
	target := m.cfg.WebRTCURL + "/" + mediaPath + "/" + string(kind)
	if resource != "" {
		target += "/" + url.PathEscape(resource)
	}

	var body io.Reader
	if r.Body != nil {
		body = http.MaxBytesReader(w, r.Body, maxSDPBytes)
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build media request")
		return
	}
	// Forward only what WHIP/WHEP needs — never the user's cookies.
	for _, h := range []string{"Content-Type", "If-Match", "Accept"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	user, pass := m.credsFor(kind)
	req.SetBasicAuth(user, pass)

	resp, err := m.client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "media server unavailable")
		return
	}
	defer resp.Body.Close()

	for _, h := range []string{"Content-Type", "ETag", "Accept-Patch"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		if u, err := url.Parse(loc); err == nil {
			w.Header().Set("Location", locationPrefix+"/"+url.PathEscape(path.Base(u.Path)))
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, io.LimitReader(resp.Body, maxSDPBytes)) //nolint:errcheck
}

// hlsHeaders are the upstream response headers passed through to viewers.
var hlsHeaders = []string{"Content-Type", "Content-Length", "ETag", "Last-Modified"}

// proxyHLS forwards an HLS playlist or segment request. rest is the file part
// after the session prefix (e.g. "index.m3u8" or "abc_seg3.mp4").
func (m *MediaServer) proxyHLS(w http.ResponseWriter, r *http.Request, mediaPath, rest string) {
	if m.cfg.HLSURL == "" || m.cfg.HLSSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "HLS not configured")
		return
	}
	target := m.cfg.HLSURL + "/" + mediaPath + "/" + rest
	if r.URL.RawQuery != "" {
		// LL-HLS blocking playlist reloads carry _HLS_msn/_HLS_part.
		target += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build media request")
		return
	}
	req.Header.Set("Authorization", "Bearer "+m.cfg.HLSSecret)
	resp, err := m.client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "media server unavailable")
		return
	}
	defer resp.Body.Close()

	for _, h := range hlsHeaders {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	// Segments sit behind session auth: keep Cloudflare and shared caches
	// from storing them regardless of what MediaMTX suggests.
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body) //nolint:errcheck
}

// PathInfo is the subset of MediaMTX's /v3/paths/get response Hytte uses.
type PathInfo struct {
	Ready   bool        `json:"ready"`
	Source  *apiObject  `json:"source"`
	Readers []apiObject `json:"readers"`
}

type apiObject struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func (m *MediaServer) apiRequest(ctx context.Context, method, p string) (*http.Response, error) {
	if m.cfg.APIURL == "" {
		return nil, fmt.Errorf("mediamtx API not configured")
	}
	req, err := http.NewRequestWithContext(ctx, method, m.cfg.APIURL+p, nil)
	if err != nil {
		return nil, err
	}
	if m.cfg.APIUser != "" {
		req.SetBasicAuth(m.cfg.APIUser, m.cfg.APIPass)
	}
	return m.client.Do(req)
}

// PathInfo returns the live state of a MediaMTX path. A path nobody has
// published to yet returns (nil, nil).
func (m *MediaServer) PathInfo(ctx context.Context, mediaPath string) (*PathInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	resp, err := m.apiRequest(ctx, http.MethodGet, "/v3/paths/get/"+mediaPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mediamtx paths/get: status %d", resp.StatusCode)
	}
	var info PathInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

// kickEndpoints maps MediaMTX connection types to their kick API collection.
var kickEndpoints = map[string]string{
	"webRTCSession": "webrtcsessions",
	"rtspSession":   "rtspsessions",
	"rtspsSession":  "rtspssessions",
	"rtmpConn":      "rtmpconns",
	"srtConn":       "srtconns",
}

// Kick disconnects the publisher and every kickable reader of a path. Used
// when a session ends so viewers and a forgotten phone stop streaming.
// Errors are best-effort: a path that is already gone is not a failure.
func (m *MediaServer) Kick(ctx context.Context, mediaPath string) error {
	info, err := m.PathInfo(ctx, mediaPath)
	if err != nil || info == nil {
		return err
	}
	objs := append([]apiObject{}, info.Readers...)
	if info.Source != nil {
		objs = append(objs, *info.Source)
	}
	var firstErr error
	for _, o := range objs {
		coll, ok := kickEndpoints[o.Type]
		if !ok || o.ID == "" {
			continue
		}
		kctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		resp, err := m.apiRequest(kctx, http.MethodPost, "/v3/"+coll+"/kick/"+url.PathEscape(o.ID))
		cancel()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		resp.Body.Close()
	}
	return firstErr
}

// SetRecording turns recording on (or off) for one path by adding (or
// removing) an exact-name path config at runtime. An exact name takes
// precedence over the ~^live/… regex in mediamtx.yml, whose default is
// record: false. Runtime config is not persisted: a MediaMTX restart stops
// recording for streams already live, which is acceptable for opt-in replays.
func (m *MediaServer) SetRecording(ctx context.Context, mediaPath string, on bool) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var (
		req *http.Request
		err error
	)
	if on {
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.APIURL+"/v3/config/paths/add/"+mediaPath, strings.NewReader(`{"record":true}`))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, http.MethodDelete, m.cfg.APIURL+"/v3/config/paths/delete/"+mediaPath, nil)
	}
	if err != nil {
		return err
	}
	if m.cfg.APIURL == "" {
		return fmt.Errorf("mediamtx API not configured")
	}
	if m.cfg.APIUser != "" {
		req.SetBasicAuth(m.cfg.APIUser, m.cfg.APIPass)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if !on && resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("mediamtx config paths: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
