package wardrobe

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Robin831/Hytte/internal/family"
)

func TestSyncLinkedChildren(t *testing.T) {
	d := setupTestDB(t)

	// Users 3 and 4 are children linked to parent 1; user 2 is an unrelated adult.
	_, err := d.Exec(`INSERT INTO users (id, email, name, picture, google_id, created_at) VALUES
		(3, 'kid1@example.com', 'Kid One', '', 'g3', ''),
		(4, 'kid2@example.com', 'Kid Two', '', 'g4', '')`)
	if err != nil {
		t.Fatalf("insert child users: %v", err)
	}
	if _, err := family.CreateLink(d, 1, 3, "Lillebror", "🦖"); err != nil {
		t.Fatalf("create link: %v", err)
	}
	if _, err := family.CreateLink(d, 1, 4, "", "⭐"); err != nil {
		t.Fatalf("create link: %v", err)
	}

	// A manually created profile (kid without an account) must survive the sync.
	manual := mustAddKid(t, d, 1, "Youngest")

	if err := SyncLinkedChildren(d, 1); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := SyncLinkedChildren(d, 1); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	kids, err := ListKids(d, 1)
	if err != nil {
		t.Fatalf("list kids: %v", err)
	}
	if len(kids) != 3 {
		t.Fatalf("got %d kids after double sync, want 3 (manual + two linked): %+v", len(kids), kids)
	}

	byUserID := map[int64]Kid{}
	for _, k := range kids {
		if k.UserID != nil {
			byUserID[*k.UserID] = k
		}
	}
	// Nickname is preferred; the account name is the fallback.
	if k, ok := byUserID[3]; !ok || k.Name != "Lillebror" || k.AvatarEmoji != "🦖" {
		t.Errorf("linked kid 3 = %+v, want nickname Lillebror with 🦖", byUserID[3])
	}
	if k, ok := byUserID[4]; !ok || k.Name != "Kid Two" {
		t.Errorf("linked kid 4 = %+v, want fallback name Kid Two", byUserID[4])
	}

	// The manual profile is untouched and still has no user link.
	found := false
	for _, k := range kids {
		if k.ID == manual.ID && k.UserID == nil && k.Name == "Youngest" {
			found = true
		}
	}
	if !found {
		t.Errorf("manual profile missing or altered after sync: %+v", kids)
	}

	// Another parent's sync must not import user 1's children.
	if err := SyncLinkedChildren(d, 2); err != nil {
		t.Fatalf("sync other parent: %v", err)
	}
	other, _ := ListKids(d, 2)
	if len(other) != 0 {
		t.Errorf("parent 2 has %d kids after sync, want 0", len(other))
	}

	// Wardrobe edits win over the link: rename sticks across a re-sync.
	linked := byUserID[3]
	if err := UpdateKid(d, linked.ID, 1, KidRequest{Name: "Storebror", AvatarEmoji: "🦕"}); err != nil {
		t.Fatalf("rename linked kid: %v", err)
	}
	if err := SyncLinkedChildren(d, 1); err != nil {
		t.Fatalf("sync after rename: %v", err)
	}
	kids, _ = ListKids(d, 1)
	for _, k := range kids {
		if k.UserID != nil && *k.UserID == 3 && k.Name != "Storebror" {
			t.Errorf("rename lost after re-sync: %+v", k)
		}
	}
}

func TestHandleListKidsImportsLinkedChildren(t *testing.T) {
	d := setupTestDB(t)

	_, err := d.Exec(`INSERT INTO users (id, email, name, picture, google_id, created_at) VALUES
		(3, 'kid1@example.com', 'Kid One', '', 'g3', '')`)
	if err != nil {
		t.Fatalf("insert child user: %v", err)
	}
	if _, err := family.CreateLink(d, 1, 3, "Minsten", "🐣"); err != nil {
		t.Fatalf("create link: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/wardrobe/kids", nil)
	w := httptest.NewRecorder()
	HandleListKids(d)(w, withUser(req, testUser))
	if w.Code != http.StatusOK {
		t.Fatalf("HandleListKids status = %d; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Kids []KidWithStats `json:"kids"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Kids) != 1 || resp.Kids[0].Name != "Minsten" {
		t.Fatalf("kids = %+v, want the linked child imported as Minsten", resp.Kids)
	}
}
