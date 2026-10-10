package races

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// kondisFixture builds a Kondis event as the API returns it.
func kondisFixture(id, title, from string, lat, lng float64, sport string, distances ...map[string]any) map[string]any {
	return map[string]any{
		"id": id, "title": title, "fromDate": from, "about": "Løp for hele familien.\r\nPåmelding på stedet.",
		"startLat": lat, "startLng": lng, "isVirtualRace": false,
		"publicEventUrl": "https://terminlista.kondis.no/events/" + id,
		"municipality":   map[string]any{"name": map[string]any{"no": "Øygarden"}},
		"sportType":      map[string]any{"slug": sport},
		"distances":      distances,
	}
}

func dist(km float64, label string, kids, uphill bool) map[string]any {
	return map[string]any{"rangeTo": km, "title": map[string]any{"no": label}, "isChildrenRace": kids, "isUpHillRace": uphill}
}

func kondisServer(t *testing.T, events *[]map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/events" || r.URL.Query().Get("fromDate") == "" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{
			"items":      *events,
			"pagination": map[string]any{"total": len(*events), "current": 1, "next": 1, "previous": 1},
		}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLocalSyncImportsNearbyRaces(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	events := []map[string]any{
		// Øygarden, ~20 km from Bergen: a 3/5 km race with a kids' race and an uphill extra.
		kondisFixture("k1", "Øygardskarusellen - Løp 6", "2026-10-12T16:00:00.000Z", 60.36, 5.03, "running_sport_type",
			dist(0.6, "Barneløp", true, false), dist(3, "3 km", false, false), dist(5, "5 km", false, false),
			dist(2, "Bakkeløp", false, true)),
		kondisFixture("k2", "Løvstien Parkrun, Bergen", "2026-10-17T08:00:00.000Z", 60.37, 5.35, "running_sport_type", dist(5, "5 km", false, false)),
		kondisFixture("k3", "Lyderhorn Opp", "2026-10-18T10:00:00.000Z", 60.37, 5.24, "running_sport_type", dist(3.8, "3,8 km", false, false)),
		kondisFixture("k4", "Sotra Sykkelritt", "2026-10-18T10:00:00.000Z", 60.30, 5.10, "cycling_sport_type", dist(40, "40 km", false, false)),
		kondisFixture("k5", "Oslo Maraton", "2026-10-19T09:00:00.000Z", 59.91, 10.75, "running_sport_type", dist(42.195, "Maraton", false, false)),
		kondisFixture("k6", "Ulriken Opp-ish", "2026-10-20T10:00:00.000Z", 60.38, 5.38, "running_sport_type", dist(4, "4 km", false, true)),
	}
	srv := kondisServer(t, &events)
	s := &LocalSyncer{DB: d, HTTP: srv.Client(), BaseURL: srv.URL, Now: func() time.Time { return time.Date(2026, 10, 10, 6, 0, 0, 0, time.UTC) }}

	st, err := s.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Fetched != 6 || st.Nearby != 1 || st.Created != 1 {
		t.Fatalf("status = %+v", st)
	}
	list, _ := ListEvents(ctx, d)
	if len(list) != 1 {
		t.Fatalf("catalog = %d races", len(list))
	}
	e := list[0]
	if e.Scope != ScopeLocal || e.Source != "kondis" || e.SourceID != "k1" || e.Place != "Øygarden" || e.Lat == nil {
		t.Fatalf("imported = %+v", e)
	}
	if e.RaceDate != "2026-10-12" || e.DistanceM != 5000 || e.Status != StatusOpen || e.Country != "NO" {
		t.Fatalf("facts = %+v", e)
	}
	if len(e.Distances) != 3 || !e.Distances[0].Kids || e.Distances[1].Kids || e.Distances[2].M != 5000 {
		t.Fatalf("distances = %+v (the uphill one must be dropped)", e.Distances)
	}
	if !strings.Contains(e.Texts["nb"].Course, "hele familien") {
		t.Fatalf("texts = %+v", e.Texts)
	}

	// Parkrun is wanted once switched on.
	set, _ := LoadResearchSettings(ctx, d)
	set.IncludeParkrun = true
	if err := SaveResearchSettings(ctx, d, set, 1); err != nil {
		t.Fatal(err)
	}
	if st, _ = s.Sync(ctx); st.Nearby != 2 || st.Created != 1 || st.Updated != 0 {
		t.Fatalf("with parkrun = %+v", st)
	}

	// A moved date is a logged change (and so announced to watchers).
	if _, err := SetWatch(ctx, d, 1, e.ID, WatchInput{State: "watching"}); err != nil {
		t.Fatal(err)
	}
	events[0]["fromDate"] = "2026-10-13T16:00:00.000Z"
	if st, _ = s.Sync(ctx); st.Updated != 1 {
		t.Fatalf("moved = %+v", st)
	}
	changes, _ := ListChanges(ctx, d, e.ID, 10)
	if len(changes) == 0 || changes[0].Field != "race_date" || changes[0].Source != "kondis" {
		t.Fatalf("changes = %+v", changes)
	}

	// Gone from Kondis: the watched race is closed, the unwatched parkrun deleted.
	// (One of two going is not "more than half", so it is trusted.)
	events = append(events[:0:0], kondisFixture("k9", "Annet løp", "2026-10-25T10:00:00.000Z", 60.39, 5.32, "running_sport_type", dist(5, "5 km", false, false)))
	events = append(events, kondisFixture("k10", "Enda et løp", "2026-10-26T10:00:00.000Z", 60.39, 5.32, "running_sport_type", dist(10, "10 km", false, false)))
	st, _ = s.Sync(ctx)
	if st.Closed != 1 || st.Removed != 1 || st.Created != 2 {
		t.Fatalf("gone = %+v", st)
	}
	if got, _ := GetEvent(ctx, d, e.ID); got.Status != StatusClosed {
		t.Fatalf("watched race should be closed, is %q", got.Status)
	}

	// A suspicious import that would drop most races removes nothing.
	events = events[:0]
	if st, _ = s.Sync(ctx); st.Removed != 0 {
		t.Fatalf("empty import removed races: %+v", st)
	}

	// Disabled: no import, but the status says why.
	set.LocalSyncEnabled = false
	_ = SaveResearchSettings(ctx, d, set, 1)
	if _, err := s.Sync(ctx); err != ErrLocalSyncDisabled {
		t.Fatalf("disabled: %v", err)
	}
	if LoadLocalSyncStatus(ctx, d).Error == "" {
		t.Fatal("status should record the error")
	}
}

func TestLocalSyncSkipsCatalogDuplicates(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	in := validInput()
	in.Name, in.RaceDate = "Bergen City Marathon, halvmaraton", "2027-04-24"
	if _, err := CreateEvent(ctx, d, in, "", "seed", 0); err != nil {
		t.Fatal(err)
	}
	events := []map[string]any{
		kondisFixture("b1", "Bergen City Marathon 2027", "2027-04-24T08:00:00.000Z", 60.39, 5.32, "running_sport_type",
			dist(21.0975, "Halvmaraton", false, false), dist(42.195, "Maraton", false, false)),
	}
	srv := kondisServer(t, &events)
	s := &LocalSyncer{DB: d, HTTP: srv.Client(), BaseURL: srv.URL, Now: func() time.Time { return time.Date(2026, 10, 10, 6, 0, 0, 0, time.UTC) }}
	if st, err := s.Sync(ctx); err != nil || st.Created != 0 || st.Nearby != 0 {
		t.Fatalf("duplicate imported: %+v, %v", st, err)
	}
}

func TestPrunePastRaces(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	mk := func(name, date string) int64 {
		in := validInput()
		in.Name, in.RaceDate = name, date
		e, err := CreateEvent(ctx, d, in, "", "test", 1)
		if err != nil {
			t.Fatal(err)
		}
		return e.ID
	}
	old := mk("Old nobody ran", "2026-09-01")
	watched := mk("Old we watched", "2026-09-01")
	withResult := mk("Old with a result", "2026-09-01")
	recent := mk("Last weekend", "2026-10-05")
	future := mk("Next year", "2027-05-09")
	if _, err := SetWatch(ctx, d, 2, watched, WatchInput{State: "skipped"}); err != nil {
		t.Fatal(err)
	}
	fin := 5000
	if _, err := SaveResult(ctx, d, 1, 0, ResultInput{RaceName: "Old with a result", RaceDate: "2026-09-01", DistanceM: 21097,
		EventID: &withResult, FinishSeconds: &fin}, "manual"); err != nil {
		t.Fatal(err)
	}

	n, err := PrunePastRaces(ctx, d, time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC))
	if err != nil || n != 1 {
		t.Fatalf("pruned %d, %v", n, err)
	}
	if _, err := GetEvent(ctx, d, old); err != ErrNotFound {
		t.Fatalf("old race still there: %v", err)
	}
	for _, id := range []int64{watched, withResult, recent, future} {
		if _, err := GetEvent(ctx, d, id); err != nil {
			t.Fatalf("race %d should stay: %v", id, err)
		}
	}
}
