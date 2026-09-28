package livestream

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func myLinkPath(t *testing.T, env *testEnv, method string) string {
	t.Helper()
	rec := env.do(t, ownerID, method, "/api/live/my-link", "", "")
	var out struct{ Path *string }
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Path == nil {
		return ""
	}
	return *out.Path
}

func TestMyLinkFollowsOwnerAcrossSessions(t *testing.T) {
	env := setupEnv(t)
	if p := myLinkPath(t, env, http.MethodGet); p != "" {
		t.Fatalf("no link yet, got %q", p)
	}
	path := myLinkPath(t, env, http.MethodPost)
	if !strings.HasPrefix(path, "/watch/") || len(path) != len("/watch/")+64 {
		t.Fatalf("unexpected link: %q", path)
	}
	if again := myLinkPath(t, env, http.MethodGet); again != path {
		t.Fatalf("GET should return the same link: %q vs %q", again, path)
	}
	var stored string
	env.db.QueryRow(`SELECT token FROM live_user_links WHERE user_id = ?`, ownerID).Scan(&stored)
	if !strings.HasPrefix(stored, "enc:") {
		t.Errorf("link token should be encrypted at rest, got %q", stored)
	}
	pub := "/api/live/public/" + strings.TrimPrefix(path, "/watch/")

	// Offline: 200 with session null and whose link it is; media is 404.
	rec := env.do(t, 0, http.MethodGet, pub, "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"session":null`) || !strings.Contains(rec.Body.String(), `"owner_name":"Owner"`) {
		t.Fatalf("offline personal link: %d %s", rec.Code, rec.Body.String())
	}
	if rec := env.do(t, 0, http.MethodPost, pub+"/whep", "application/sdp", "v=0"); rec.Code != http.StatusNotFound {
		t.Errorf("whep while offline: want 404, got %d", rec.Code)
	}
	if rec := env.do(t, 0, http.MethodGet, pub+"/track", "", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"points":[]`) {
		t.Errorf("track while offline: %d %s", rec.Code, rec.Body.String())
	}

	// Live: the same URL shows the current session and can be watched.
	first := env.goLive(t, ownerID, "Run one")
	rec = env.do(t, 0, http.MethodGet, pub, "", "")
	if !strings.Contains(rec.Body.String(), `"title":"Run one"`) {
		t.Fatalf("personal link should show the live session: %s", rec.Body.String())
	}
	if rec := env.do(t, 0, http.MethodPost, pub+"/whep", "application/sdp", "v=0\r\noffer\r\n"); rec.Code != http.StatusCreated {
		t.Fatalf("whep via personal link: %d", rec.Code)
	}

	// Next broadcast: the link moves along to the new session.
	env.goLive(t, ownerID, "Run two")
	rec = env.do(t, 0, http.MethodGet, pub, "", "")
	if !strings.Contains(rec.Body.String(), `"title":"Run two"`) {
		t.Fatalf("personal link should follow to the newest session: %s", rec.Body.String())
	}
	if got, _ := GetSession(env.db, first.ID); got.Status != StatusEnded {
		t.Fatal("first session should have ended when the second started")
	}

	// Another user's live session never shows through this link.
	env.goLive(t, viewerID, "Not Owner's")
	rec = env.do(t, 0, http.MethodGet, pub, "", "")
	if strings.Contains(rec.Body.String(), "Not Owner") {
		t.Fatal("personal link leaked another user's stream")
	}

	// Reset: old URL dies, new one works.
	newPath := myLinkPath(t, env, http.MethodPost)
	if newPath == path {
		t.Fatal("reset should issue a new link")
	}
	if rec := env.do(t, 0, http.MethodGet, pub, "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("old link after reset: want 404, got %d", rec.Code)
	}
	// Delete: link gone.
	if rec := env.do(t, ownerID, http.MethodDelete, "/api/live/my-link", "", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec := env.do(t, 0, http.MethodGet, "/api/live/public/"+strings.TrimPrefix(newPath, "/watch/"), "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("deleted link: want 404, got %d", rec.Code)
	}
}

func TestMyLinkRequiresFeature(t *testing.T) {
	env := setupEnv(t)
	if rec := env.do(t, noFeatID, http.MethodPost, "/api/live/my-link", "", ""); rec.Code != http.StatusForbidden {
		t.Errorf("my-link without feature: want 403, got %d", rec.Code)
	}
}

// A phone that reloads or comes back from the background publishes into the
// same, still-live session again (MediaMTX's overridePublisher replaces the
// stale connection).
func TestOwnerCanRepublishToLiveSession(t *testing.T) {
	env := setupEnv(t)
	s := env.goLive(t, ownerID, "")
	for i := 0; i < 2; i++ {
		if rec := env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/whip"), "application/sdp", "v=0\r\noffer\r\n"); rec.Code != http.StatusCreated {
			t.Fatalf("WHIP attempt %d: %d", i+1, rec.Code)
		}
	}
	if got, _ := GetSession(env.db, s.ID); got.Status != StatusLive {
		t.Fatal("session should still be live after re-publishing")
	}
}
