package livestream

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func (e *testEnv) goLiveWith(t *testing.T, userID int64, body string) sessionResponse {
	t.Helper()
	rec := e.do(t, userID, http.MethodPost, "/api/live/sessions", "application/json", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Session sessionResponse `json:"session"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	return out.Session
}

func getSessionResp(t *testing.T, e *testEnv, userID, id int64) sessionResponse {
	t.Helper()
	rec := e.do(t, userID, http.MethodGet, sessionPath(id, ""), "", "")
	var out struct {
		Session sessionResponse `json:"session"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	return out.Session
}

// --- #2 notifications ----------------------------------------------------

func TestGoLiveNotifiesOtherFeatureUsers(t *testing.T) {
	env := setupEnv(t)
	env.goLiveWith(t, ownerID, `{"title":"Tempo run","notify":true}`)
	if len(env.pushed) != 1 || env.pushed[0].userID != viewerID {
		t.Fatalf("expected exactly one push to the viewer, got %+v", env.pushed)
	}
	var n struct{ Title, Body, URL, Tag string }
	json.Unmarshal([]byte(env.pushed[0].payload), &n)
	if n.Title != "Owner is live" || !strings.Contains(n.Body, "Tempo run") || !strings.HasPrefix(n.URL, "/live/") {
		t.Errorf("unexpected notification: %+v", n)
	}
}

func TestGoLiveWithoutNotifySendsNothing(t *testing.T) {
	env := setupEnv(t)
	env.goLiveWith(t, ownerID, `{"notify":false}`)
	if len(env.pushed) != 0 {
		t.Fatalf("expected no pushes, got %+v", env.pushed)
	}
}

func TestGoLiveNotifyRespectsQuietHours(t *testing.T) {
	env := setupEnv(t)
	// Quiet hours covering the whole day for the viewer.
	for k, v := range map[string]string{"quiet_hours_enabled": "true", "quiet_hours_start": "00:00", "quiet_hours_end": "23:59", "quiet_hours_timezone": "UTC"} {
		env.db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (?, ?, ?)`, viewerID, k, v)
	}
	env.goLiveWith(t, ownerID, `{"notify":true}`)
	for _, p := range env.pushed {
		if p.userID == viewerID {
			t.Fatal("viewer in quiet hours should not be notified")
		}
	}
}

// --- #6 heartbeat status -------------------------------------------------

func TestHeartbeatReportsBatteryAndState(t *testing.T) {
	env := setupEnv(t)
	s := env.goLive(t, ownerID, "")
	rec := env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/heartbeat"), "application/json",
		`{"battery_level":0.42,"battery_charging":true,"state":"reconnecting"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("heartbeat: %d %s", rec.Code, rec.Body.String())
	}
	got := getSessionResp(t, env, viewerID, s.ID)
	if got.BatteryLevel == nil || *got.BatteryLevel != 0.42 || !got.BatteryCharging || got.PublisherState != "reconnecting" {
		t.Errorf("status not reported: %+v", got)
	}
	// Garbage state and out-of-range battery are dropped, not stored.
	env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/heartbeat"), "application/json", `{"battery_level":7,"state":"<script>"}`)
	got = getSessionResp(t, env, viewerID, s.ID)
	if got.BatteryLevel != nil || got.PublisherState != "" {
		t.Errorf("invalid status stored: %+v", got)
	}
}

// --- #5 share links ------------------------------------------------------

func TestShareLinkPublicAccess(t *testing.T) {
	env := setupEnv(t)
	s := env.goLive(t, ownerID, "Parkrun")

	if rec := env.do(t, viewerID, http.MethodPost, sessionPath(s.ID, "/share"), "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner share: want 404, got %d", rec.Code)
	}
	rec := env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/share"), "", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("share: %d %s", rec.Code, rec.Body.String())
	}
	var share struct{ Token, Path string }
	json.Unmarshal(rec.Body.Bytes(), &share)
	if len(share.Token) != 64 || share.Path != "/watch/"+share.Token {
		t.Fatalf("unexpected share response: %+v", share)
	}
	var stored string
	env.db.QueryRow(`SELECT share_token_hash FROM live_sessions WHERE id = ?`, s.ID).Scan(&stored)
	if stored == share.Token || stored != hashShareToken(share.Token) {
		t.Error("share token must be stored hashed")
	}
	if !getSessionResp(t, env, ownerID, s.ID).HasShareLink {
		t.Error("owner should see has_share_link")
	}

	// Anonymous viewer (no cookie) can see status, get ICE and play.
	pub := "/api/live/public/" + share.Token
	rec = env.do(t, 0, http.MethodGet, pub, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("public get: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"owner_name":"Owner"`) || strings.Contains(body, "user_id") || strings.Contains(body, `"is_owner":true`) {
		t.Errorf("public response leaks or mislabels: %s", body)
	}
	if rec := env.do(t, 0, http.MethodGet, pub+"/ice", "", ""); rec.Code != http.StatusOK {
		t.Errorf("public ice: %d", rec.Code)
	}
	rec = env.do(t, 0, http.MethodPost, pub+"/whep", "application/sdp", "v=0\r\noffer\r\n")
	if rec.Code != http.StatusCreated {
		t.Fatalf("public whep: %d %s", rec.Code, rec.Body.String())
	}
	if up := env.media.last(); up.User != "reader" || !strings.HasSuffix(up.Path, "/whep") {
		t.Errorf("unexpected upstream: %+v", up)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, pub+"/whep/") {
		t.Errorf("Location not rewritten to public prefix: %q", loc)
	}
	if rec := env.do(t, 0, http.MethodGet, pub+"/hls/index.m3u8", "", ""); rec.Code != http.StatusOK {
		t.Errorf("public hls: %d", rec.Code)
	}
	// Publishing is never possible through a share link.
	if rec := env.do(t, 0, http.MethodPost, pub+"/whip", "application/sdp", "v=0"); rec.Code == http.StatusCreated {
		t.Error("share link must not allow WHIP")
	}

	// Bad and revoked tokens are 404.
	if rec := env.do(t, 0, http.MethodGet, "/api/live/public/"+strings.Repeat("0", 64), "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown token: want 404, got %d", rec.Code)
	}
	if rec := env.do(t, 0, http.MethodGet, "/api/live/public/nothex", "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("malformed token: want 404, got %d", rec.Code)
	}
	if rec := env.do(t, ownerID, http.MethodDelete, sessionPath(s.ID, "/share"), "", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d", rec.Code)
	}
	if rec := env.do(t, 0, http.MethodGet, pub, "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("revoked token: want 404, got %d", rec.Code)
	}
}

func TestShareLinkStopsWhenStreamEnds(t *testing.T) {
	env := setupEnv(t)
	s := env.goLive(t, ownerID, "")
	rec := env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/share"), "", "")
	var share struct{ Token string }
	json.Unmarshal(rec.Body.Bytes(), &share)
	env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/end"), "", "")

	pub := "/api/live/public/" + share.Token
	rec = env.do(t, 0, http.MethodGet, pub, "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ended"`) {
		t.Errorf("ended public status: %d %s", rec.Code, rec.Body.String())
	}
	for _, p := range []string{"/ice"} {
		if rec := env.do(t, 0, http.MethodGet, pub+p, "", ""); rec.Code != http.StatusGone {
			t.Errorf("GET %s after end: want 410, got %d", p, rec.Code)
		}
	}
	if rec := env.do(t, 0, http.MethodPost, pub+"/whep", "application/sdp", "v=0"); rec.Code != http.StatusGone {
		t.Errorf("public whep after end: want 410, got %d", rec.Code)
	}
}

// --- #1 GPS track ---------------------------------------------------------

func TestTrackRequiresLocationOption(t *testing.T) {
	env := setupEnv(t)
	s := env.goLive(t, ownerID, "")
	rec := env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/track"), "application/json",
		`{"points":[{"t":"2026-09-28T12:00:00Z","lat":59.9,"lon":10.7}]}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("track without location option: want 403, got %d", rec.Code)
	}
}

func TestTrackPostAndIncrementalGet(t *testing.T) {
	env := setupEnv(t)
	s := env.goLiveWith(t, ownerID, `{"location":true}`)
	if rec := env.do(t, viewerID, http.MethodPost, sessionPath(s.ID, "/track"), "application/json", `{"points":[]}`); rec.Code != http.StatusNotFound {
		t.Errorf("non-owner track post: want 404, got %d", rec.Code)
	}
	rec := env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/track"), "application/json", `{"points":[
		{"t":"2026-09-28T12:00:00Z","lat":59.91,"lon":10.75,"alt":12.5,"acc":5},
		{"t":"2026-09-28T12:00:05Z","lat":59.9105,"lon":10.7502,"acc":6},
		{"t":"2026-09-28T12:00:10Z","lat":123,"lon":10.7},
		{"t":"not-a-time","lat":59.9,"lon":10.7}
	]}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"stored":2`) {
		t.Fatalf("track post: %d %s", rec.Code, rec.Body.String())
	}
	var raw string
	env.db.QueryRow(`SELECT point FROM live_track_points LIMIT 1`).Scan(&raw)
	if !strings.HasPrefix(raw, "enc:") || strings.Contains(raw, "59.91") {
		t.Errorf("track point not encrypted at rest: %q", raw)
	}

	var out struct{ Points []TrackPoint }
	rec = env.do(t, viewerID, http.MethodGet, sessionPath(s.ID, "/track"), "", "")
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Points) != 2 || out.Points[0].Lat != 59.91 || out.Points[0].Alt == nil || *out.Points[0].Alt != 12.5 {
		t.Fatalf("unexpected track: %+v", out.Points)
	}
	rec = env.do(t, viewerID, http.MethodGet, sessionPath(s.ID, "/track")+"?after="+jsonInt(out.Points[0].ID), "", "")
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Points) != 1 || out.Points[0].Lat != 59.9105 {
		t.Errorf("incremental track: %+v", out.Points)
	}
}

func jsonInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestTrackPurgedAfterUnrecordedSessionEnds(t *testing.T) {
	env := setupEnv(t)
	s := env.goLiveWith(t, ownerID, `{"location":true}`)
	env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/track"), "application/json",
		`{"points":[{"t":"2026-09-28T12:00:00Z","lat":59.91,"lon":10.75}]}`)
	env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/end"), "", "")

	count := func() int {
		var n int
		env.db.QueryRow(`SELECT COUNT(*) FROM live_track_points WHERE session_id = ?`, s.ID).Scan(&n)
		return n
	}
	env.h.reapOnce()
	if count() != 1 {
		t.Fatal("track purged too early — viewers still on the page should see the route")
	}
	env.now = env.now.Add(trackPurgeDelay + time.Minute)
	env.h.reapOnce()
	if count() != 0 {
		t.Fatal("track of an unrecorded session should be purged after the delay")
	}
}

// --- #3 recording ------------------------------------------------------------

func TestRecordRefusedWhenUnavailable(t *testing.T) {
	env := setupEnv(t)
	rec := env.do(t, ownerID, http.MethodPost, "/api/live/sessions", "application/json", `{"record":true}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("record without recorder: want 503, got %d %s", rec.Code, rec.Body.String())
	}
}

// enableRecorder points the test recorder at temp dirs. It needs ffmpeg on
// PATH to be "enabled"; tests that only exercise bookkeeping fake the path.
func enableRecorder(t *testing.T, env *testEnv) (segRoot, outDir string) {
	t.Helper()
	segRoot, outDir = t.TempDir(), t.TempDir()
	r := env.h.recorder
	r.SegmentsDir, r.OutDir = segRoot, outDir
	r.FFmpeg, _ = exec.LookPath("ffmpeg")
	r.FFprobe, _ = exec.LookPath("ffprobe")
	if r.FFmpeg == "" {
		r.FFmpeg = "/nonexistent/ffmpeg"
	}
	r.MinFreeBytes = 1
	r.flushDelay = 0
	r.now = func() time.Time { return env.now }
	return segRoot, outDir
}

func TestRecordingStartsAndFailsCleanlyWithoutSegments(t *testing.T) {
	env := setupEnv(t)
	enableRecorder(t, env)
	s := env.goLiveWith(t, ownerID, `{"record":true}`)
	if !s.Recording {
		t.Fatal("session should report recording")
	}
	var key string
	env.db.QueryRow(`SELECT stream_key FROM live_sessions WHERE id = ?`, s.ID).Scan(&key)
	if len(env.media.configCalls) != 1 || env.media.configCalls[0] != `POST /v3/config/paths/add/live/`+key+` {"record":true}` {
		t.Fatalf("recording not enabled on the path: %v", env.media.configCalls)
	}
	env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/end"), "", "")
	if last := env.media.configCalls[len(env.media.configCalls)-1]; !strings.HasPrefix(last, "DELETE /v3/config/paths/delete/live/"+key) {
		t.Errorf("recording not switched off at end: %v", env.media.configCalls)
	}
	rec, err := GetRecordingBySession(env.db, s.ID)
	if err != nil || rec.Status != RecStatusFailed || rec.Error == "" {
		t.Fatalf("expected failed recording with no segments, got %+v %v", rec, err)
	}
}

func TestRecordingRemuxLinksWorkoutAndServesVideo(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	env := setupEnv(t)
	segRoot, outDir := enableRecorder(t, env)
	s := env.goLiveWith(t, ownerID, `{"record":true,"title":"Long run"}`)

	// Two fragmented-MP4 segments like MediaMTX writes (H.264 + Opus).
	var key string
	env.db.QueryRow(`SELECT stream_key FROM live_sessions WHERE id = ?`, s.ID).Scan(&key)
	segDir := filepath.Join(segRoot, "live", key)
	os.MkdirAll(segDir, 0o755)
	for i, name := range []string{"2026-09-28_12-00-00-000000.mp4", "2026-09-28_12-00-02-000000.mp4"} {
		out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
			"-f", "lavfi", "-i", "testsrc=size=320x180:rate=15:duration=2",
			"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
			"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "libopus", "-strict", "-2",
			"-movflags", "frag_keyframe+empty_moov+default_base_moof",
			filepath.Join(segDir, name)).CombinedOutput()
		if err != nil {
			t.Skipf("cannot generate test segment %d (ffmpeg build lacks x264/opus?): %v %s", i, err, out)
		}
	}
	// A workout that overlaps the broadcast.
	env.db.Exec(`INSERT INTO workouts (id, user_id, sport, title, started_at, duration_seconds, distance_meters, created_at)
		VALUES (77, ?, 'running', 'Sunday long run', ?, 3600, 15000, '')`, ownerID, formatTime(env.now.Add(-2*time.Minute)))

	env.now = env.now.Add(10 * time.Minute)
	if rec := env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/end"), "", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("end: %d", rec.Code)
	}
	rec, err := GetRecordingBySession(env.db, s.ID)
	if err != nil || rec.Status != RecStatusReady {
		t.Fatalf("recording not ready: %+v %v", rec, err)
	}
	if rec.DurationSeconds < 3.5 || rec.SizeBytes == 0 {
		t.Errorf("unexpected duration/size: %+v", rec)
	}
	if _, err := os.Stat(segDir); !os.IsNotExist(err) {
		t.Error("segments should be deleted after a successful remux")
	}
	if rec.WorkoutID == nil || *rec.WorkoutID != 77 {
		t.Errorf("workout not linked: %+v", rec.WorkoutID)
	}

	// Viewer sees it in the list with the workout (not linkable for them).
	resp := env.do(t, viewerID, http.MethodGet, "/api/live/recordings", "", "")
	if !strings.Contains(resp.Body.String(), `"title":"Long run"`) || !strings.Contains(resp.Body.String(), `"linkable":false`) {
		t.Errorf("recording list: %s", resp.Body.String())
	}
	// Range request works (seeking).
	req := httptest.NewRequest(http.MethodGet, "/api/live/recordings/"+jsonInt(rec.ID)+"/video", nil)
	req.Header.Set("Range", "bytes=0-99")
	req.AddCookie(&http.Cookie{Name: "session", Value: env.tokens[viewerID]})
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusPartialContent || w.Body.Len() != 100 || w.Header().Get("Content-Type") != "video/mp4" {
		t.Errorf("range request: %d len=%d type=%s", w.Code, w.Body.Len(), w.Header().Get("Content-Type"))
	}

	// Only the owner can delete; deleting removes the file.
	if r := env.do(t, viewerID, http.MethodDelete, "/api/live/recordings/"+jsonInt(rec.ID), "", ""); r.Code != http.StatusNotFound {
		t.Errorf("non-owner delete: want 404, got %d", r.Code)
	}
	if r := env.do(t, ownerID, http.MethodDelete, "/api/live/recordings/"+jsonInt(rec.ID), "", ""); r.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", r.Code)
	}
	if entries, _ := os.ReadDir(outDir); len(entries) != 0 {
		t.Errorf("replay file not removed: %v", entries)
	}
}

func TestRecordedSessionKeepsTrack(t *testing.T) {
	env := setupEnv(t)
	enableRecorder(t, env)
	s := env.goLiveWith(t, ownerID, `{"record":true,"location":true}`)
	env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/track"), "application/json",
		`{"points":[{"t":"2026-09-28T12:00:00Z","lat":59.91,"lon":10.75}]}`)
	env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/end"), "", "")
	env.now = env.now.Add(24 * time.Hour)
	env.h.reapOnce()
	var n int
	env.db.QueryRow(`SELECT COUNT(*) FROM live_track_points WHERE session_id = ?`, s.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("recorded session's track should be kept for the replay, got %d points", n)
	}
}

func TestLinkWorkoutPicksLargestOverlap(t *testing.T) {
	env := setupEnv(t)
	s := env.goLive(t, ownerID, "")
	start := env.now
	env.now = env.now.Add(time.Hour)
	EndSession(env.db, s.ID, env.now)
	env.db.Exec(`INSERT INTO live_recordings (session_id, user_id, status) VALUES (?, ?, 'ready')`, s.ID, ownerID)
	_, err := env.db.Exec(`INSERT INTO workouts (id, user_id, sport, title, started_at, duration_seconds, fit_file_hash, created_at) VALUES
		(1, ?, 'running', 'warmup', ?, 600, 'h1', ''),
		(2, ?, 'running', 'main', ?, 3000, 'h2', ''),
		(3, ?, 'running', 'yesterday', ?, 3600, 'h3', ''),
		(4, ?, 'running', 'someone else', ?, 3600, 'h4', '')`,
		ownerID, formatTime(start.Add(-20*time.Minute)),
		ownerID, formatTime(start.Add(5*time.Minute)),
		ownerID, formatTime(start.Add(-24*time.Hour)),
		viewerID, formatTime(start))
	if err != nil {
		t.Fatalf("insert workouts: %v", err)
	}
	id, err := LinkWorkout(env.db, s.ID)
	if err != nil || id != 2 {
		t.Fatalf("LinkWorkout = %d, %v; want 2", id, err)
	}
}
