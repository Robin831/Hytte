package offers

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

var testUser = &auth.User{ID: 1, Email: "a@example.com", Name: "A"}

func TestHandleListRanksPerUser(t *testing.T) {
	d := setupTestDB(t)
	seed := []Offer{
		testOffer("milk", 20, 30),
		testOffer("pizza", 40, 80),
	}
	seed[0].Heading = "TINE HELMELK"
	seed[1].Heading = "Pizza"
	if err := UpsertOffers(context.Background(), d, seed); err != nil {
		t.Fatalf("seed offers: %v", err)
	}
	if _, err := AddWatchlist(d, 1, "melk"); err != nil {
		t.Fatalf("seed watchlist: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/offers", nil)
	w := httptest.NewRecorder()
	HandleList(d)(w, withUser(req, testUser))
	if w.Code != http.StatusOK {
		t.Fatalf("HandleList status = %d; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Offers    []RankedOffer    `json:"offers"`
		Watchlist []WatchlistEntry `json:"watchlist"`
		FetchedAt *string          `json:"fetched_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Offers) != 2 || len(resp.Watchlist) != 1 || resp.FetchedAt == nil {
		t.Fatalf("resp = %d offers, %d watchlist, fetched_at %v", len(resp.Offers), len(resp.Watchlist), resp.FetchedAt)
	}
	// The watched milk outranks the pizza despite pizza's 50% discount.
	if resp.Offers[0].ID != "milk" || len(resp.Offers[0].MatchedKeywords) != 1 {
		t.Errorf("offers[0] = %+v, want watched milk first", resp.Offers[0])
	}
	if resp.Offers[1].DiscountPct != 50 {
		t.Errorf("offers[1] discount = %d, want 50", resp.Offers[1].DiscountPct)
	}

	// A user with no watchlist sees pure discount order.
	req = httptest.NewRequest(http.MethodGet, "/api/offers", nil)
	w = httptest.NewRecorder()
	HandleList(d)(w, withUser(req, &auth.User{ID: 2}))
	var resp2 struct {
		Offers []RankedOffer `json:"offers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp2.Offers[0].ID != "pizza" {
		t.Errorf("user 2 offers[0] = %+v, want pizza first", resp2.Offers[0])
	}
}

func TestHandleWatchlistFlow(t *testing.T) {
	d := setupTestDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/offers/watchlist", bytes.NewBufferString(`{"keyword":" vaskemiddel "}`))
	w := httptest.NewRecorder()
	HandleAddWatch(d)(w, withUser(req, testUser))
	if w.Code != http.StatusCreated {
		t.Fatalf("add status = %d; body: %s", w.Code, w.Body.String())
	}
	var addResp struct {
		Entry WatchlistEntry `json:"entry"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &addResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if addResp.Entry.Keyword != "vaskemiddel" {
		t.Errorf("keyword = %q, want trimmed vaskemiddel", addResp.Entry.Keyword)
	}

	// Duplicate → 409.
	req = httptest.NewRequest(http.MethodPost, "/api/offers/watchlist", bytes.NewBufferString(`{"keyword":"VASKEMIDDEL"}`))
	w = httptest.NewRecorder()
	HandleAddWatch(d)(w, withUser(req, testUser))
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate status = %d, want 409", w.Code)
	}

	// Empty → 400.
	req = httptest.NewRequest(http.MethodPost, "/api/offers/watchlist", bytes.NewBufferString(`{"keyword":"  "}`))
	w = httptest.NewRecorder()
	HandleAddWatch(d)(w, withUser(req, testUser))
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty status = %d, want 400", w.Code)
	}

	// Delete by another user → 404; by owner → 200.
	req = httptest.NewRequest(http.MethodDelete, "/api/offers/watchlist/1", nil)
	req = withURLParam(withUser(req, &auth.User{ID: 2}), "id", "1")
	w = httptest.NewRecorder()
	HandleDeleteWatch(d)(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("other-user delete status = %d, want 404", w.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/offers/watchlist/1", nil)
	req = withURLParam(withUser(req, testUser), "id", "1")
	w = httptest.NewRecorder()
	HandleDeleteWatch(d)(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("owner delete status = %d, want 200", w.Code)
	}
}
