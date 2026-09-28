package livestream

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/familychat"
	"github.com/go-chi/chi/v5"
)

// Heartbeat timing. The broadcaster page pings every HeartbeatInterval while
// live; a session with no ping for StaleAfter is ended by the reaper (closed
// tab, dead battery, phone out of coverage for too long).
const (
	HeartbeatInterval = 15 * time.Second
	StaleAfter        = 2 * time.Minute
	reapInterval      = 30 * time.Second
)

// hlsFilePattern restricts the HLS proxy to the flat file names MediaMTX
// serves (index.m3u8, <id>_stream.m3u8, <id>_init.mp4, <id>_seg12.mp4, …).
var hlsFilePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+\.(m3u8|mp4|m4s|ts)$`)

// resourcePattern matches the MediaMTX WHIP/WHEP session id (a UUID).
var resourcePattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// Handlers serves the /api/live endpoints.
type Handlers struct {
	db    *sql.DB
	cfg   Config
	media *MediaServer
	ice   familychat.WebRTCConfig
	now   func() time.Time
}

// NewHandlers builds the handler set from explicit config (tests) — use
// RegisterRoutes in production, which reads the environment.
func NewHandlers(db *sql.DB, cfg Config, ice familychat.WebRTCConfig) *Handlers {
	return &Handlers{db: db, cfg: cfg, media: NewMediaServer(cfg), ice: ice, now: time.Now}
}

// RegisterRoutes mounts the livestream API, gated by the "livestream" feature.
// Must be called inside the RequireAuth group.
func RegisterRoutes(r chi.Router, db *sql.DB) {
	NewHandlers(db, ConfigFromEnv(), familychat.LoadWebRTCConfig()).Mount(r)
}

// Mount registers the routes on r.
func (h *Handlers) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireFeature(h.db, "livestream"))
		r.Get("/live/ice", h.HandleICE)
		r.Get("/live/sessions", h.HandleList)
		r.Post("/live/sessions", h.HandleCreate)
		r.Get("/live/sessions/{id}", h.HandleGet)
		r.Post("/live/sessions/{id}/heartbeat", h.HandleHeartbeat)
		r.Post("/live/sessions/{id}/end", h.HandleEnd)

		r.Post("/live/sessions/{id}/whip", h.signal(kindWHIP))
		r.Patch("/live/sessions/{id}/whip/{resource}", h.signal(kindWHIP))
		r.Delete("/live/sessions/{id}/whip/{resource}", h.signal(kindWHIP))
		r.Post("/live/sessions/{id}/whep", h.signal(kindWHEP))
		r.Patch("/live/sessions/{id}/whep/{resource}", h.signal(kindWHEP))
		r.Delete("/live/sessions/{id}/whep/{resource}", h.signal(kindWHEP))

		r.Get("/live/sessions/{id}/hls/{file}", h.HandleHLS)
	})
}

// sessionResponse is the wire shape of a session. The stream key is
// deliberately absent — clients address sessions by id only.
type sessionResponse struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	OwnerName string `json:"owner_name"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at,omitempty"`
	IsOwner   bool   `json:"is_owner"`
	// OnAir is true once the phone's media is actually flowing into MediaMTX.
	// A session can be live but not yet (or temporarily not) on air.
	OnAir   bool `json:"on_air"`
	Viewers int  `json:"viewers"`
}

func (h *Handlers) toResponse(ctx context.Context, s *Session, userID int64) sessionResponse {
	resp := sessionResponse{
		ID:        s.ID,
		UserID:    s.UserID,
		OwnerName: s.OwnerName,
		Title:     s.Title,
		Status:    s.Status,
		StartedAt: formatTime(s.StartedAt),
		EndedAt:   formatTime(s.EndedAt),
		IsOwner:   s.UserID == userID,
	}
	if s.Status == StatusLive && h.cfg.APIURL != "" {
		info, err := h.media.PathInfo(ctx, s.MediaPath())
		if err != nil {
			log.Printf("livestream: path info for session %d: %v", s.ID, err)
		} else if info != nil {
			resp.OnAir = info.Ready
			for _, rd := range info.Readers {
				// HLS viewers share one muxer, so only WebRTC readers are
				// individually countable.
				if rd.Type == "webRTCSession" {
					resp.Viewers++
				}
			}
		}
	}
	return resp
}

// HandleICE returns STUN/TURN servers (with short-lived coturn credentials)
// for RTCPeerConnection — the same config Family Chat calls use.
func (h *Handlers) HandleICE(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	identity := "live"
	if user != nil {
		identity = "live-" + strconv.FormatInt(user.ID, 10)
	}
	writeJSON(w, http.StatusOK, familychat.BuildICEConfig(h.ice, identity, h.now()))
}

// HandleList returns every session currently live.
func (h *Handlers) HandleList(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	sessions, err := ListLive(h.db)
	if err != nil {
		log.Printf("livestream: list: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list live sessions")
		return
	}
	out := make([]sessionResponse, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, h.toResponse(r.Context(), s, user.ID))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sessions":           out,
		"configured":         h.cfg.Enabled(),
		"heartbeat_interval": int(HeartbeatInterval.Seconds()),
	})
}

// HandleCreate starts a new broadcast for the caller.
func (h *Handlers) HandleCreate(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "livestreaming is not configured on this server")
		return
	}
	user := auth.UserFromContext(r.Context())
	var body struct {
		Title string `json:"title"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	title := strings.TrimSpace(body.Title)
	if utf8.RuneCountInString(title) > maxTitleLen {
		writeError(w, http.StatusBadRequest, "title is too long")
		return
	}
	s, previous, err := CreateSession(h.db, user.ID, title, h.now())
	if err != nil {
		log.Printf("livestream: create for user %d: %v", user.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to start live session")
		return
	}
	for _, p := range previous {
		h.kick(p)
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"session":            h.toResponse(r.Context(), s, user.ID),
		"heartbeat_interval": int(HeartbeatInterval.Seconds()),
	})
}

// HandleGet returns one session (live or ended) so a viewer can tell when the
// stream they are watching has finished.
func (h *Handlers) HandleGet(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadSession(w, r)
	if !ok {
		return
	}
	user := auth.UserFromContext(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"session": h.toResponse(r.Context(), s, user.ID)})
}

// HandleHeartbeat keeps the caller's own live session from being reaped.
// Returns 409 once the session has ended so the broadcaster can stop.
func (h *Handlers) HandleHeartbeat(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadOwnSession(w, r)
	if !ok {
		return
	}
	if s.Status != StatusLive {
		writeError(w, http.StatusConflict, "session has ended")
		return
	}
	if err := Touch(h.db, s.ID, h.now()); err != nil {
		log.Printf("livestream: heartbeat %d: %v", s.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to record heartbeat")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleEnd ends the caller's own session and disconnects everyone from it.
func (h *Handlers) HandleEnd(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadOwnSession(w, r)
	if !ok {
		return
	}
	if err := EndSession(h.db, s.ID, h.now()); err != nil {
		log.Printf("livestream: end %d: %v", s.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to end session")
		return
	}
	h.kick(s)
	w.WriteHeader(http.StatusNoContent)
}

// signal proxies WHIP (owner only) and WHEP (any feature user) signalling.
// New connections need a live session; DELETE (hang-up) is always allowed so
// clients can clean up after the session has ended.
func (h *Handlers) signal(kind signalKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var s *Session
		var ok bool
		if kind == kindWHIP {
			s, ok = h.loadOwnSession(w, r)
		} else {
			s, ok = h.loadSession(w, r)
		}
		if !ok {
			return
		}
		if !h.cfg.Enabled() {
			writeError(w, http.StatusServiceUnavailable, "livestreaming is not configured on this server")
			return
		}
		if s.Status != StatusLive && r.Method != http.MethodDelete {
			writeError(w, http.StatusGone, "session has ended")
			return
		}
		resource := chi.URLParam(r, "resource")
		if resource != "" && !resourcePattern.MatchString(resource) {
			writeError(w, http.StatusBadRequest, "invalid resource id")
			return
		}
		prefix := "/api/live/sessions/" + strconv.FormatInt(s.ID, 10) + "/" + string(kind)
		h.media.proxySignal(w, r, kind, s.MediaPath(), resource, prefix)
	}
}

// HandleHLS proxies HLS playlists and segments for viewers whose network
// cannot do WebRTC.
func (h *Handlers) HandleHLS(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadSession(w, r)
	if !ok {
		return
	}
	if s.Status != StatusLive {
		writeError(w, http.StatusGone, "session has ended")
		return
	}
	file := chi.URLParam(r, "file")
	if !hlsFilePattern.MatchString(file) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	h.media.proxyHLS(w, r, s.MediaPath(), file)
}

func (h *Handlers) loadSession(w http.ResponseWriter, r *http.Request) (*Session, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return nil, false
	}
	s, err := GetSession(h.db, id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "session not found")
		return nil, false
	}
	if err != nil {
		log.Printf("livestream: load session %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to load session")
		return nil, false
	}
	return s, true
}

// loadOwnSession is loadSession restricted to the caller's own sessions.
// Another user's session reads as 404 so ids cannot be probed for ownership.
func (h *Handlers) loadOwnSession(w http.ResponseWriter, r *http.Request) (*Session, bool) {
	s, ok := h.loadSession(w, r)
	if !ok {
		return nil, false
	}
	user := auth.UserFromContext(r.Context())
	if user == nil || s.UserID != user.ID {
		writeError(w, http.StatusNotFound, "session not found")
		return nil, false
	}
	return s, true
}

// kick disconnects a session's publisher and viewers, best effort.
func (h *Handlers) kick(s *Session) {
	if h.cfg.APIURL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.media.Kick(ctx, s.MediaPath()); err != nil {
		log.Printf("livestream: kick session %d: %v", s.ID, err)
	}
}

// StartReaper ends sessions whose broadcaster stopped sending heartbeats and
// kicks their MediaMTX connections. Blocks until ctx is cancelled.
func StartReaper(ctx context.Context, db *sql.DB) {
	h := NewHandlers(db, ConfigFromEnv(), familychat.WebRTCConfig{})
	ticker := time.NewTicker(reapInterval)
	defer ticker.Stop()
	for {
		h.reapOnce()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (h *Handlers) reapOnce() {
	now := h.now()
	stale, err := EndStale(h.db, now.Add(-StaleAfter), now)
	if err != nil {
		log.Printf("livestream: reap stale sessions: %v", err)
		return
	}
	for _, s := range stale {
		log.Printf("livestream: ended stale session %d (no heartbeat since %s)", s.ID, formatTime(s.LastSeenAt))
		h.kick(s)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("livestream: encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
