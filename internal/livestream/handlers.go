package livestream

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/familychat"
	"github.com/go-chi/chi/v5"
)

// Heartbeat timing. The broadcaster page pings every HeartbeatInterval while
// live; a session with no ping for StaleAfter is ended by the reaper (dead
// battery, phone out of coverage for too long). Phones suspend background
// tabs, so this is generous enough to survive a quick switch to another app —
// the page resumes the same session when it comes back.
const (
	HeartbeatInterval = 15 * time.Second
	StaleAfter        = 5 * time.Minute
	reapInterval      = 30 * time.Second
)

// hlsFilePattern restricts the HLS proxy to the flat file names MediaMTX
// serves (index.m3u8, <id>_stream.m3u8, <id>_init.mp4, <id>_seg12.mp4, …).
var hlsFilePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+\.(m3u8|mp4|m4s|ts)$`)

// resourcePattern matches the MediaMTX WHIP/WHEP session id (a UUID).
var resourcePattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// Handlers serves the /api/live endpoints.
type Handlers struct {
	db       *sql.DB
	cfg      Config
	media    *MediaServer
	recorder *Recorder
	ice      familychat.WebRTCConfig
	push     PushFunc
	now      func() time.Time
	// async runs background work (notifications, replay builds); tests make
	// it synchronous.
	async func(func())
}

// NewHandlers builds the handler set from explicit config (tests). Production
// code uses Default, which reads the environment.
func NewHandlers(db *sql.DB, cfg Config, ice familychat.WebRTCConfig) *Handlers {
	media := NewMediaServer(cfg)
	return &Handlers{
		db:       db,
		cfg:      cfg,
		media:    media,
		recorder: &Recorder{db: db, media: media, MinFreeBytes: defaultMinFreeBytes, now: time.Now, flushDelay: flushDelay},
		ice:      ice,
		push:     defaultPush,
		now:      time.Now,
		async:    func(f func()) { go f() },
	}
}

var (
	defaultOnce     sync.Once
	defaultHandlers *Handlers
)

// Default returns the process-wide handler set, built from the environment
// on first use. The router and the reaper share it so replay builds are
// serialised by a single Recorder.
func Default(db *sql.DB) *Handlers {
	defaultOnce.Do(func() {
		h := NewHandlers(db, ConfigFromEnv(), familychat.LoadWebRTCConfig())
		h.recorder = RecorderFromEnv(db, h.media)
		defaultHandlers = h
	})
	return defaultHandlers
}

// RegisterRoutes mounts the authenticated livestream API, gated by the
// "livestream" feature. Must be called inside the RequireAuth group.
func RegisterRoutes(r chi.Router, db *sql.DB) {
	Default(db).Mount(r)
}

// RegisterPublicRoutes mounts the share-link endpoints. Must be called
// outside the auth groups: the share token is the credential.
func RegisterPublicRoutes(r chi.Router, db *sql.DB) {
	Default(db).MountPublic(r)
}

// Mount registers the authenticated routes on r.
func (h *Handlers) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireFeature(h.db, "livestream"))
		r.Get("/live/ice", h.HandleICE)
		r.Get("/live/my-link", h.HandleMyLinkGet)
		r.Post("/live/my-link", h.HandleMyLinkCreate)
		r.Delete("/live/my-link", h.HandleMyLinkDelete)
		r.Get("/live/sessions", h.HandleList)
		r.Post("/live/sessions", h.HandleCreate)
		r.Get("/live/sessions/{id}", h.HandleGet)
		r.Post("/live/sessions/{id}/heartbeat", h.HandleHeartbeat)
		r.Post("/live/sessions/{id}/end", h.HandleEnd)
		r.Post("/live/sessions/{id}/share", h.HandleShareCreate)
		r.Delete("/live/sessions/{id}/share", h.HandleShareDelete)
		r.Post("/live/sessions/{id}/track", h.HandleTrackPost)
		r.Get("/live/sessions/{id}/track", h.HandleTrackGet)

		r.Post("/live/sessions/{id}/whip", h.signal(kindWHIP))
		r.Patch("/live/sessions/{id}/whip/{resource}", h.signal(kindWHIP))
		r.Delete("/live/sessions/{id}/whip/{resource}", h.signal(kindWHIP))
		r.Post("/live/sessions/{id}/whep", h.signal(kindWHEP))
		r.Patch("/live/sessions/{id}/whep/{resource}", h.signal(kindWHEP))
		r.Delete("/live/sessions/{id}/whep/{resource}", h.signal(kindWHEP))

		r.Get("/live/sessions/{id}/hls/{file}", h.HandleHLS)

		r.Get("/live/recordings", h.HandleRecordingList)
		r.Get("/live/recordings/{id}", h.HandleRecordingGet)
		r.Get("/live/recordings/{id}/video", h.HandleRecordingVideo)
		r.Get("/live/recordings/{id}/track", h.HandleRecordingTrack)
		r.Delete("/live/recordings/{id}", h.HandleRecordingDelete)
	})
}

// MountPublic registers the share-link routes on r (no auth).
func (h *Handlers) MountPublic(r chi.Router) {
	r.Get("/live/public/{token}", h.HandlePublicGet)
	r.Get("/live/public/{token}/ice", h.HandlePublicICE)
	r.Get("/live/public/{token}/track", h.HandlePublicTrack)
	r.Post("/live/public/{token}/whep", h.publicWHEP)
	r.Patch("/live/public/{token}/whep/{resource}", h.publicWHEP)
	r.Delete("/live/public/{token}/whep/{resource}", h.publicWHEP)
	r.Get("/live/public/{token}/hls/{file}", h.HandlePublicHLS)
}

// sessionResponse is the wire shape of a session. The stream key and share
// token hash are deliberately absent.
type sessionResponse struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id,omitempty"`
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

	Recording bool `json:"recording"`
	Location  bool `json:"location"`
	// PublisherState is the broadcaster's own view of its connection
	// ("live", "reconnecting", …) from the last heartbeat.
	PublisherState  string   `json:"publisher_state,omitempty"`
	BatteryLevel    *float64 `json:"battery_level,omitempty"`
	BatteryCharging bool     `json:"battery_charging,omitempty"`
	// HasShareLink is only reported to the owner.
	HasShareLink bool `json:"has_share_link,omitempty"`
}

func (h *Handlers) toResponse(ctx context.Context, s *Session, userID int64) sessionResponse {
	resp := sessionResponse{
		ID:              s.ID,
		UserID:          s.UserID,
		OwnerName:       s.OwnerName,
		Title:           s.Title,
		Status:          s.Status,
		StartedAt:       formatTime(s.StartedAt),
		EndedAt:         formatTime(s.EndedAt),
		IsOwner:         userID != 0 && s.UserID == userID,
		Recording:       s.Record,
		Location:        s.Location,
		PublisherState:  s.PublisherState,
		BatteryLevel:    s.BatteryLevel,
		BatteryCharging: s.BatteryCharging,
	}
	if resp.IsOwner {
		resp.HasShareLink = s.ShareTokenHash != ""
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

// publicResponse trims a session for anonymous share-link viewers.
func (h *Handlers) publicResponse(ctx context.Context, s *Session) sessionResponse {
	resp := h.toResponse(ctx, s, 0)
	resp.UserID = 0
	resp.OwnerName = firstName(s.OwnerName)
	return resp
}

func firstName(name string) string {
	if f := strings.Fields(name); len(f) > 0 {
		return f[0]
	}
	return ""
}

func (h *Handlers) iceFor(identity string) familychat.ICEConfig {
	return familychat.BuildICEConfig(h.ice, identity, h.now())
}

// HandleICE returns STUN/TURN servers (with short-lived coturn credentials)
// for RTCPeerConnection — the same config Family Chat calls use.
func (h *Handlers) HandleICE(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	identity := "live"
	if user != nil {
		identity = "live-" + strconv.FormatInt(user.ID, 10)
	}
	writeJSON(w, http.StatusOK, h.iceFor(identity))
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
		"sessions":            out,
		"configured":          h.cfg.Enabled(),
		"recording_available": h.recorder.Enabled(),
		"heartbeat_interval":  int(HeartbeatInterval.Seconds()),
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
		Title    string `json:"title"`
		Notify   bool   `json:"notify"`
		Record   bool   `json:"record"`
		Location bool   `json:"location"`
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
	if body.Record {
		if err := h.recorder.CanRecord(); err != nil {
			status := http.StatusServiceUnavailable
			if errors.Is(err, ErrLowDiskSpace) {
				status = http.StatusInsufficientStorage
			}
			writeError(w, status, err.Error())
			return
		}
	}
	opts := Options{Notify: body.Notify, Record: body.Record, Location: body.Location}
	s, previous, err := CreateSession(h.db, user.ID, title, opts, h.now())
	if err != nil {
		log.Printf("livestream: create for user %d: %v", user.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to start live session")
		return
	}
	for _, p := range previous {
		h.onEnded(p)
	}
	if s.Record {
		if err := h.recorder.Start(r.Context(), s); err != nil {
			log.Printf("livestream: start recording for session %d: %v", s.ID, err)
		}
	}
	if s.Notify {
		sess := *s
		h.async(func() { notifyGoLive(h.db, h.push, &sess) })
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
	resp := map[string]any{"session": h.toResponse(r.Context(), s, user.ID)}
	if s.Status == StatusEnded && s.Record {
		if rec, err := GetRecordingBySession(h.db, s.ID); err == nil {
			resp["recording_id"] = rec.ID
			resp["recording_status"] = rec.Status
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleHeartbeat keeps the caller's own live session from being reaped and
// records the broadcaster's battery and connection state for viewers.
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
	var hb Heartbeat
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&hb); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	if err := Touch(h.db, s.ID, hb, h.now()); err != nil {
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
	if s.Status == StatusLive {
		if err := EndSession(h.db, s.ID, h.now()); err != nil {
			log.Printf("livestream: end %d: %v", s.ID, err)
			writeError(w, http.StatusInternalServerError, "failed to end session")
			return
		}
		s.Status = StatusEnded
		s.EndedAt = h.now()
		h.onEnded(s)
	}
	w.WriteHeader(http.StatusNoContent)
}

// onEnded runs after a session ends: disconnect everyone and, if it was
// recorded, build the replay in the background.
func (h *Handlers) onEnded(s *Session) {
	h.kick(s)
	if s.Record && h.recorder.Enabled() {
		sess := *s
		h.async(func() { h.recorder.Finish(&sess) })
	}
}

// HandleShareCreate issues (or rotates) the public share link for the
// caller's live session. The token is returned once; only its hash is kept.
func (h *Handlers) HandleShareCreate(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadOwnSession(w, r)
	if !ok {
		return
	}
	if s.Status != StatusLive {
		writeError(w, http.StatusGone, "session has ended")
		return
	}
	token, hash, err := newShareToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create share link")
		return
	}
	if err := SetShareTokenHash(h.db, s.ID, hash); err != nil {
		log.Printf("livestream: share %d: %v", s.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to create share link")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"token": token, "path": "/watch/" + token})
}

// HandleShareDelete revokes the share link.
func (h *Handlers) HandleShareDelete(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadOwnSession(w, r)
	if !ok {
		return
	}
	if err := SetShareTokenHash(h.db, s.ID, ""); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke share link")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleTrackPost stores GPS fixes from the broadcaster's phone.
func (h *Handlers) HandleTrackPost(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadOwnSession(w, r)
	if !ok {
		return
	}
	if s.Status != StatusLive {
		writeError(w, http.StatusConflict, "session has ended")
		return
	}
	if !s.Location {
		writeError(w, http.StatusForbidden, "location sharing is off for this broadcast")
		return
	}
	var body struct {
		Points []TrackPoint `json:"points"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(body.Points) > maxPointsPerPost {
		writeError(w, http.StatusBadRequest, "too many points")
		return
	}
	n, err := AddTrackPoints(h.db, s.ID, body.Points)
	if err != nil {
		log.Printf("livestream: track %d: %v", s.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to store track")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"stored": n})
}

// HandleTrackGet returns the session's GPS track after ?after=<id>.
func (h *Handlers) HandleTrackGet(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadSession(w, r)
	if !ok {
		return
	}
	h.serveTrack(w, r, s)
}

func (h *Handlers) serveTrack(w http.ResponseWriter, r *http.Request, s *Session) {
	if !s.Location {
		writeJSON(w, http.StatusOK, map[string]any{"points": []TrackPoint{}})
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	points, err := ListTrackPoints(h.db, s.ID, after)
	if err != nil {
		log.Printf("livestream: list track %d: %v", s.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load track")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"points": points})
}

// signal proxies WHIP (owner only) and WHEP (any feature user) signalling.
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
		prefix := "/api/live/sessions/" + strconv.FormatInt(s.ID, 10) + "/" + string(kind)
		h.proxySignal(w, r, kind, s, prefix)
	}
}

// proxySignal runs the checks shared by the member and share-link routes.
// New connections need a live session; DELETE (hang-up) is always allowed so
// clients can clean up after the session has ended.
func (h *Handlers) proxySignal(w http.ResponseWriter, r *http.Request, kind signalKind, s *Session, prefix string) {
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
	h.media.proxySignal(w, r, kind, s.MediaPath(), resource, prefix)
}

// HandleHLS proxies HLS playlists and segments for viewers whose network
// cannot do WebRTC.
func (h *Handlers) HandleHLS(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadSession(w, r)
	if !ok {
		return
	}
	h.serveHLS(w, r, s)
}

func (h *Handlers) serveHLS(w http.ResponseWriter, r *http.Request, s *Session) {
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

// --- share-link (public) routes ------------------------------------------

// resolveToken resolves a share token (per-session or personal link).
// Unknown, malformed and revoked tokens all read as 404. s is nil when a
// personal link's owner is not live right now.
func (h *Handlers) resolveToken(w http.ResponseWriter, r *http.Request) (s *Session, ownerName string, ok bool) {
	token := chi.URLParam(r, "token")
	if !shareTokenPattern.MatchString(token) {
		writeError(w, http.StatusNotFound, "stream not found")
		return nil, "", false
	}
	s, ownerName, err := resolveShare(h.db, token)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "stream not found")
		return nil, "", false
	}
	if err != nil {
		log.Printf("livestream: resolve share token: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load stream")
		return nil, "", false
	}
	return s, ownerName, true
}

// loadShared is resolveToken for media routes: an offline personal link is
// "not live" (404) there.
func (h *Handlers) loadShared(w http.ResponseWriter, r *http.Request) (*Session, bool) {
	s, _, ok := h.resolveToken(w, r)
	if !ok {
		return nil, false
	}
	if s == nil {
		writeError(w, http.StatusNotFound, "not live right now")
		return nil, false
	}
	return s, true
}

// HandlePublicGet returns what a share link currently shows. For a personal
// link whose owner is offline, session is null and owner_name says who the
// page is waiting for.
func (h *Handlers) HandlePublicGet(w http.ResponseWriter, r *http.Request) {
	s, ownerName, ok := h.resolveToken(w, r)
	if !ok {
		return
	}
	if s == nil {
		writeJSON(w, http.StatusOK, map[string]any{"session": nil, "owner_name": firstName(ownerName)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": h.publicResponse(r.Context(), s), "owner_name": firstName(ownerName)})
}

// HandleMyLinkGet returns the caller's personal live link, if they have one.
func (h *Handlers) HandleMyLinkGet(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	link, err := GetUserLink(h.db, user.ID)
	if errors.Is(err, ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"path": nil})
		return
	}
	if err != nil {
		log.Printf("livestream: get my link for %d: %v", user.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load link")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": "/watch/" + link.Token})
}

// HandleMyLinkCreate creates or resets the caller's personal live link.
func (h *Handlers) HandleMyLinkCreate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	link, err := CreateUserLink(h.db, user.ID, h.now())
	if err != nil {
		log.Printf("livestream: create my link for %d: %v", user.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to create link")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"path": "/watch/" + link.Token})
}

// HandleMyLinkDelete turns the caller's personal live link off.
func (h *Handlers) HandleMyLinkDelete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if err := DeleteUserLink(h.db, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete link")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandlePublicICE hands TURN credentials to share-link viewers, only while
// the stream is live.
func (h *Handlers) HandlePublicICE(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadShared(w, r)
	if !ok {
		return
	}
	if s.Status != StatusLive {
		writeError(w, http.StatusGone, "session has ended")
		return
	}
	writeJSON(w, http.StatusOK, h.iceFor("live-share-"+strconv.FormatInt(s.ID, 10)))
}

// HandlePublicTrack serves the GPS track to share-link viewers while live.
func (h *Handlers) HandlePublicTrack(w http.ResponseWriter, r *http.Request) {
	s, _, ok := h.resolveToken(w, r)
	if !ok {
		return
	}
	if s == nil || s.Status != StatusLive {
		writeJSON(w, http.StatusOK, map[string]any{"points": []TrackPoint{}})
		return
	}
	h.serveTrack(w, r, s)
}

func (h *Handlers) publicWHEP(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadShared(w, r)
	if !ok {
		return
	}
	prefix := "/api/live/public/" + chi.URLParam(r, "token") + "/whep"
	h.proxySignal(w, r, kindWHEP, s, prefix)
}

// HandlePublicHLS proxies HLS for share-link viewers.
func (h *Handlers) HandlePublicHLS(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadShared(w, r)
	if !ok {
		return
	}
	h.serveHLS(w, r, s)
}

// --- recordings ------------------------------------------------------------

type workoutSummary struct {
	ID             int64   `json:"id"`
	Title          string  `json:"title"`
	Sport          string  `json:"sport"`
	DistanceMeters float64 `json:"distance_meters"`
	// Linkable is true for the owner, who can open /training/{id}.
	Linkable bool `json:"linkable"`
}

type recordingResponse struct {
	ID              int64           `json:"id"`
	SessionID       int64           `json:"session_id"`
	OwnerName       string          `json:"owner_name"`
	Title           string          `json:"title"`
	StartedAt       string          `json:"started_at"`
	EndedAt         string          `json:"ended_at,omitempty"`
	Status          string          `json:"status"`
	Error           string          `json:"error,omitempty"`
	DurationSeconds float64         `json:"duration_seconds"`
	SizeBytes       int64           `json:"size_bytes"`
	IsOwner         bool            `json:"is_owner"`
	HasTrack        bool            `json:"has_track"`
	Workout         *workoutSummary `json:"workout,omitempty"`
}

func (h *Handlers) recordingResponse(rec *Recording, userID int64) (recordingResponse, error) {
	s, err := GetSession(h.db, rec.SessionID)
	if err != nil {
		return recordingResponse{}, err
	}
	resp := recordingResponse{
		ID:              rec.ID,
		SessionID:       rec.SessionID,
		OwnerName:       s.OwnerName,
		Title:           s.Title,
		StartedAt:       formatTime(s.StartedAt),
		EndedAt:         formatTime(s.EndedAt),
		Status:          rec.Status,
		Error:           rec.Error,
		DurationSeconds: rec.DurationSeconds,
		SizeBytes:       rec.SizeBytes,
		IsOwner:         rec.UserID == userID,
		HasTrack:        s.Location,
	}
	if rec.WorkoutID != nil {
		var ws workoutSummary
		err := h.db.QueryRow(`SELECT id, title, sport, distance_meters FROM workouts WHERE id = ?`, *rec.WorkoutID).
			Scan(&ws.ID, &ws.Title, &ws.Sport, &ws.DistanceMeters)
		if err == nil {
			ws.Linkable = resp.IsOwner
			resp.Workout = &ws
		}
	}
	return resp, nil
}

// HandleRecordingList returns all replays plus free space on the volume.
func (h *Handlers) HandleRecordingList(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	recs, err := ListRecordings(h.db)
	if err != nil {
		log.Printf("livestream: list recordings: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list recordings")
		return
	}
	out := make([]recordingResponse, 0, len(recs))
	var used int64
	for _, rec := range recs {
		used += rec.SizeBytes
		resp, err := h.recordingResponse(rec, user.ID)
		if err != nil {
			continue
		}
		out = append(out, resp)
	}
	result := map[string]any{"recordings": out, "used_bytes": used}
	if h.recorder.Enabled() {
		if free, err := h.recorder.DiskUsage(); err == nil {
			result["free_bytes"] = free
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) loadRecording(w http.ResponseWriter, r *http.Request) (*Recording, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid recording id")
		return nil, false
	}
	rec, err := GetRecording(h.db, id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "recording not found")
		return nil, false
	}
	if err != nil {
		log.Printf("livestream: load recording %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to load recording")
		return nil, false
	}
	return rec, true
}

// HandleRecordingGet returns one replay, linking it to a workout first if the
// watch has synced since the replay was built.
func (h *Handlers) HandleRecordingGet(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.loadRecording(w, r)
	if !ok {
		return
	}
	if rec.Status == RecStatusReady && rec.WorkoutID == nil {
		if id, err := LinkWorkout(h.db, rec.SessionID); err == nil && id != 0 {
			rec.WorkoutID = &id
		}
	}
	user := auth.UserFromContext(r.Context())
	resp, err := h.recordingResponse(rec, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load recording")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recording": resp})
}

// HandleRecordingVideo streams the replay MP4 with range support (seeking).
func (h *Handlers) HandleRecordingVideo(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.loadRecording(w, r)
	if !ok {
		return
	}
	if rec.Status != RecStatusReady {
		writeError(w, http.StatusConflict, "recording is not ready")
		return
	}
	p, err := h.recorder.FilePath(rec)
	if err != nil {
		writeError(w, http.StatusNotFound, "recording not found")
		return
	}
	f, err := os.Open(p)
	if err != nil {
		writeError(w, http.StatusNotFound, "recording file missing")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read recording")
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, rec.FileName, info.ModTime(), f)
}

// HandleRecordingTrack returns the GPS route kept with a replay.
func (h *Handlers) HandleRecordingTrack(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.loadRecording(w, r)
	if !ok {
		return
	}
	s, err := GetSession(h.db, rec.SessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "recording not found")
		return
	}
	h.serveTrack(w, r, s)
}

// HandleRecordingDelete removes the caller's own replay.
func (h *Handlers) HandleRecordingDelete(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.loadRecording(w, r)
	if !ok {
		return
	}
	user := auth.UserFromContext(r.Context())
	if rec.UserID != user.ID {
		writeError(w, http.StatusNotFound, "recording not found")
		return
	}
	if rec.Status == RecStatusRecording || rec.Status == RecStatusProcessing {
		writeError(w, http.StatusConflict, "recording is still being processed")
		return
	}
	if err := h.recorder.DeleteRecording(rec); err != nil {
		log.Printf("livestream: delete recording %d: %v", rec.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to delete recording")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---------------------------------------------------------------

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

// StartReaper ends sessions whose broadcaster stopped sending heartbeats,
// finishes replays interrupted by a restart, and purges expired GPS tracks.
// Blocks until ctx is cancelled.
func StartReaper(ctx context.Context, db *sql.DB) {
	h := Default(db)
	h.recorder.ResumePending()
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
	}
	for _, s := range stale {
		log.Printf("livestream: ended stale session %d (no heartbeat since %s)", s.ID, formatTime(s.LastSeenAt))
		h.onEnded(s)
	}
	if err := PurgeExpiredTracks(h.db, now); err != nil {
		log.Printf("livestream: purge tracks: %v", err)
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
