package wardrobe

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

func withUser(r *http.Request, user *auth.User) *http.Request {
	return r.WithContext(auth.ContextWithUser(r.Context(), user))
}

func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

var testUser = &auth.User{ID: 1, Email: "parent@example.com", Name: "Parent"}

func TestHandleKidsFlow(t *testing.T) {
	d := setupTestDB(t)

	// Add a kid.
	req := httptest.NewRequest(http.MethodPost, "/api/wardrobe/kids", bytes.NewBufferString(`{"name":"Ola","avatar_emoji":"🧒","birthdate":"2021-05-01"}`))
	w := httptest.NewRecorder()
	HandleAddKid(d)(w, withUser(req, testUser))
	if w.Code != http.StatusCreated {
		t.Fatalf("HandleAddKid status = %d; body: %s", w.Code, w.Body.String())
	}
	var addResp struct {
		Kid Kid `json:"kid"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &addResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Missing name is rejected.
	req = httptest.NewRequest(http.MethodPost, "/api/wardrobe/kids", bytes.NewBufferString(`{"name":"  "}`))
	w = httptest.NewRecorder()
	HandleAddKid(d)(w, withUser(req, testUser))
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty name status = %d, want 400", w.Code)
	}

	// Add a measurement via handler.
	kidID := addResp.Kid.ID
	req = httptest.NewRequest(http.MethodPost, "/api/wardrobe/kids/1/measurements", bytes.NewBufferString(`{"measured_at":"2026-07-01","height_cm":100,"foot_length_mm":160}`))
	req = withURLParam(withUser(req, testUser), "id", "1")
	w = httptest.NewRecorder()
	HandleAddMeasurement(d)(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("HandleAddMeasurement status = %d; body: %s", w.Code, w.Body.String())
	}

	// List kids includes stats derived from the measurement.
	req = httptest.NewRequest(http.MethodGet, "/api/wardrobe/kids", nil)
	w = httptest.NewRecorder()
	HandleListKids(d)(w, withUser(req, testUser))
	if w.Code != http.StatusOK {
		t.Fatalf("HandleListKids status = %d", w.Code)
	}
	var listResp struct {
		Kids []KidWithStats `json:"kids"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(listResp.Kids) != 1 {
		t.Fatalf("got %d kids, want 1", len(listResp.Kids))
	}
	k := listResp.Kids[0]
	if k.ID != kidID || k.Clothing == nil || k.Clothing.BuySize != 104 || k.Shoe == nil || k.Shoe.BuyEU != 27 {
		t.Errorf("kid stats = %+v, want buy size 104 and EU 27", k)
	}

	// Another user cannot add measurements to this kid.
	req = httptest.NewRequest(http.MethodPost, "/api/wardrobe/kids/1/measurements", bytes.NewBufferString(`{"measured_at":"2026-07-01","height_cm":90}`))
	req = withURLParam(withUser(req, &auth.User{ID: 2}), "id", "1")
	w = httptest.NewRecorder()
	HandleAddMeasurement(d)(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("other user's measurement status = %d, want 404", w.Code)
	}
}

func TestHandleListCategoriesSeedsDefaults(t *testing.T) {
	d := setupTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/wardrobe/categories", nil)
	w := httptest.NewRecorder()
	HandleListCategories(d)(w, withUser(req, testUser))
	if w.Code != http.StatusOK {
		t.Fatalf("HandleListCategories status = %d", w.Code)
	}
	var resp struct {
		Categories []Category `json:"categories"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Categories) != len(defaultCategories) {
		t.Errorf("first list seeded %d categories, want %d", len(resp.Categories), len(defaultCategories))
	}
}

func TestHandleItemsAndNeeds(t *testing.T) {
	d := setupTestDB(t)
	kid := mustAddKid(t, d, 1, "Ola")
	if err := SeedDefaultCategories(d, 1); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cats, _ := ListCategories(d, 1)
	var wool Category
	for _, c := range cats {
		if c.Name == "Ullundertøy" {
			wool = c
		}
	}

	// Add item with defaults filled in by the handler.
	body, _ := json.Marshal(ItemRequest{KidID: kid.ID, CategoryID: wool.ID, Name: "Ullsett", SizeLabel: "98"})
	req := httptest.NewRequest(http.MethodPost, "/api/wardrobe/items", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	HandleAddItem(d)(w, withUser(req, testUser))
	if w.Code != http.StatusCreated {
		t.Fatalf("HandleAddItem status = %d; body: %s", w.Code, w.Body.String())
	}
	var addResp struct {
		Item Item `json:"item"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &addResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if addResp.Item.Quantity != 1 || addResp.Item.Status != "active" || addResp.Item.Condition != "good" {
		t.Errorf("defaults not applied: %+v", addResp.Item)
	}

	// Invalid enum rejected.
	req = httptest.NewRequest(http.MethodPost, "/api/wardrobe/items", bytes.NewBufferString(`{"kid_id":1,"category_id":`+itoa(wool.ID)+`,"name":"X","status":"lost"}`))
	w = httptest.NewRecorder()
	HandleAddItem(d)(w, withUser(req, testUser))
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid status code = %d, want 400", w.Code)
	}

	// Needs: wool target 2, one active item → shortfall of 1.
	req = httptest.NewRequest(http.MethodGet, "/api/wardrobe/needs?kid_id=1", nil)
	w = httptest.NewRecorder()
	HandleNeeds(d)(w, withUser(req, testUser))
	if w.Code != http.StatusOK {
		t.Fatalf("HandleNeeds status = %d; body: %s", w.Code, w.Body.String())
	}
	var needsResp struct {
		Needs    []NeedEntry     `json:"needs"`
		TooSmall []TooSmallEntry `json:"too_small"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &needsResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	found := false
	for _, n := range needsResp.Needs {
		if n.CategoryID == wool.ID {
			found = true
			if n.Have != 1 || n.Target != 2 {
				t.Errorf("wool need = %+v, want have 1 / target 2", n)
			}
		}
	}
	if !found {
		t.Error("expected a need entry for Ullundertøy")
	}

	// Needs for someone else's kid → 404.
	req = httptest.NewRequest(http.MethodGet, "/api/wardrobe/needs?kid_id=1", nil)
	w = httptest.NewRecorder()
	HandleNeeds(d)(w, withUser(req, &auth.User{ID: 2}))
	if w.Code != http.StatusNotFound {
		t.Errorf("other user's needs status = %d, want 404", w.Code)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
