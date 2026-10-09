package races

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/go-chi/chi/v5"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-races-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })
	database, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`INSERT INTO users (id, email, name, picture, google_id, created_at) VALUES
		(1, 'a@example.com', 'A', '', 'g1', ''), (2, 'b@example.com', 'B', '', 'g2', '')`); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	return database
}

func validInput() EventInput {
	return EventInput{
		Name: "Test Half", RaceDate: "2027-05-09", Country: "no", DistanceM: 21097,
		Status: StatusOpen, EntryType: "fcfs", Travel: "direct", URL: "https://example.com/half",
		Series: []string{"superhalfs"},
		Texts: map[string]EventText{
			"nb": {Place: "Bergen, Norge", How: "Åpen."},
			"en": {Place: "Bergen, Norway", How: "Open."},
		},
	}
}

func TestSeedCatalogLoadsOnceAndRespectsDeletes(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	if err := SeedCatalog(ctx, d); err != nil {
		t.Fatalf("seed: %v", err)
	}
	events, err := ListEvents(ctx, d)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) < 50 {
		t.Fatalf("seeded %d events, want the full starting catalog", len(events))
	}

	deadlines := 0
	for _, e := range events {
		deadlines += len(e.Deadlines)
		for _, lang := range Languages {
			if e.Texts[lang].Place == "" {
				t.Errorf("%s: missing %s place text", e.Slug, lang)
			}
		}
	}
	if deadlines == 0 {
		t.Fatal("seed loaded no deadlines")
	}

	// London belongs to both series; Sydney's lottery close has an exact instant.
	var london, sydney *Event
	for i := range events {
		switch events[i].Slug {
		case "tcs-london-marathon-2027":
			london = &events[i]
		case "tcs-sydney-marathon-2027":
			sydney = &events[i]
		}
	}
	if london == nil || strings.Join(london.Series, ",") != "majors,emc" {
		t.Fatalf("london = %+v, want series majors,emc", london)
	}
	if sydney == nil {
		t.Fatal("sydney missing")
	}
	var closes *Deadline
	for i := range sydney.Deadlines {
		if sydney.Deadlines[i].Kind == "lottery_closes" {
			closes = &sydney.Deadlines[i]
		}
	}
	// 10:00 in Sydney (AEDT, UTC+11) on 19 Oct 2026 is 23:00 UTC on the 18th.
	if closes == nil || closes.DueAt == nil || closes.DueAt.Format("2006-01-02T15:04Z07:00") != "2026-10-18T23:00Z" {
		t.Fatalf("sydney lottery close = %+v, want due_at 2026-10-18T23:00Z", closes)
	}

	// A deleted seed race is not resurrected by the next startup.
	if err := DeleteEvent(ctx, d, london.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := SeedCatalog(ctx, d); err != nil {
		t.Fatalf("reseed: %v", err)
	}
	again, _ := ListEvents(ctx, d)
	if len(again) != len(events)-1 {
		t.Fatalf("after delete + reseed: %d events, want %d", len(again), len(events)-1)
	}
}

func TestEventInputValidation(t *testing.T) {
	cases := map[string]func(*EventInput){
		"empty name":      func(in *EventInput) { in.Name = " " },
		"bad date":        func(in *EventInput) { in.RaceDate = "2027-02-30" },
		"bad status":      func(in *EventInput) { in.Status = "maybe" },
		"bad entry type":  func(in *EventInput) { in.EntryType = "raffle" },
		"bad travel":      func(in *EventInput) { in.Travel = "boat" },
		"bad series":      func(in *EventInput) { in.Series = []string{"nope"} },
		"bad country":     func(in *EventInput) { in.Country = "NOR" },
		"zero distance":   func(in *EventInput) { in.DistanceM = 0 },
		"javascript url":  func(in *EventInput) { in.URL = "javascript:alert(1)" },
		"unknown lang":    func(in *EventInput) { in.Texts["de"] = EventText{Place: "x"} },
		"bad precision":   func(in *EventInput) { in.DatePrecision = "someday" },
		"oversized texts": func(in *EventInput) { in.Texts["nb"] = EventText{How: strings.Repeat("a", maxTextLen+1)} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := validInput()
			mutate(&in)
			if err := in.Normalize(); !errors.Is(err, ErrValidation) {
				t.Fatalf("Normalize() = %v, want validation error", err)
			}
		})
	}

	in := validInput()
	if err := in.Normalize(); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	if in.Country != "NO" || in.EditionYear != 2027 || in.DatePrecision != "day" {
		t.Fatalf("normalized = %+v", in)
	}
}

func TestDeadlineInputValidation(t *testing.T) {
	ok := DeadlineInput{Kind: "lottery_closes", DueDate: "2026-10-29", DueTime: "14:00", TZ: "America/Chicago"}
	if err := ok.Normalize(); err != nil {
		t.Fatalf("valid deadline rejected: %v", err)
	}
	for name, in := range map[string]DeadlineInput{
		"time without tz": {Kind: "other", DueDate: "2026-10-29", DueTime: "14:00"},
		"bad tz":          {Kind: "other", DueDate: "2026-10-29", DueTime: "14:00", TZ: "Mars/Olympus"},
		"bad time":        {Kind: "other", DueDate: "2026-10-29", DueTime: "25:00", TZ: "Europe/Oslo"},
		"bad kind":        {Kind: "whenever", DueDate: "2026-10-29"},
	} {
		if err := in.Normalize(); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: Normalize() = %v, want validation error", name, err)
		}
	}
}

func TestUpdateEventLogsChangesAndKeepsOtherLanguages(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	created, err := CreateEvent(ctx, d, validInput(), "", "manual", 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Slug != "test-half-2027" {
		t.Fatalf("slug = %q", created.Slug)
	}

	// An edit that only sends English changes status and the English text.
	edit := validInput()
	edit.Status = StatusClosed
	edit.Texts = map[string]EventText{"en": {Place: "Bergen, Norway", How: "Sold out."}}
	updated, diffs, err := UpdateEvent(ctx, d, created.ID, edit, "manual", 1)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Status != StatusClosed || updated.Texts["en"].How != "Sold out." {
		t.Fatalf("updated = %+v", updated)
	}
	if updated.Texts["nb"].How != "Åpen." {
		t.Fatalf("nb text = %q, want it kept when the edit didn't send nb", updated.Texts["nb"].How)
	}

	fields := map[string]bool{}
	for _, df := range diffs {
		fields[df.field] = true
	}
	if len(diffs) != 2 || !fields["status"] || !fields["texts.en.how"] {
		t.Fatalf("diffs = %+v, want status and texts.en.how", diffs)
	}

	changes, err := ListChanges(ctx, d, created.ID, 10)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	// created + 2 field changes, newest first.
	if len(changes) != 3 || changes[len(changes)-1].Field != "created" {
		t.Fatalf("changes = %+v", changes)
	}

	// A no-op save logs nothing new.
	if _, diffs, err := UpdateEvent(ctx, d, created.ID, edit, "manual", 1); err != nil || len(diffs) != 0 {
		t.Fatalf("no-op update: diffs=%v err=%v", diffs, err)
	}
	// A second race with the same name gets its own slug.
	second, err := CreateEvent(ctx, d, validInput(), "", "manual", 1)
	if err != nil || second.Slug != "test-half-2027-2" {
		t.Fatalf("second slug = %v, err %v", second, err)
	}
}

func TestDeadlineCRUDLogsChanges(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	e, err := CreateEvent(ctx, d, validInput(), "", "manual", 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	dl, err := CreateDeadline(ctx, d, e.ID, DeadlineInput{Kind: "entry_closes", DueDate: "2027-04-01",
		Texts: map[string]DeadlineText{"nb": {What: "Stenger."}}}, "manual", 1)
	if err != nil {
		t.Fatalf("create deadline: %v", err)
	}
	moved, err := UpdateDeadline(ctx, d, dl.ID, DeadlineInput{Kind: "entry_closes", DueDate: "2027-04-15"}, "manual", 1)
	if err != nil {
		t.Fatalf("update deadline: %v", err)
	}
	if moved.DueDate != "2027-04-15" || moved.Texts["nb"].What != "Stenger." {
		t.Fatalf("moved = %+v, want new date and kept nb text", moved)
	}
	if err := DeleteDeadline(ctx, d, dl.ID, "manual", 1); err != nil {
		t.Fatalf("delete deadline: %v", err)
	}
	changes, _ := ListChanges(ctx, d, e.ID, 10)
	var got []string
	for _, c := range changes {
		got = append(got, c.Field+":"+c.OldValue+">"+c.NewValue)
	}
	want := []string{
		"deadline.entry_closes:2027-04-15>",
		"deadline.entry_closes:2027-04-01>2027-04-15",
		"deadline.entry_closes:>2027-04-01",
		"created:>Test Half",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("changes =\n%v\nwant\n%v", got, want)
	}
	if _, err := CreateDeadline(ctx, d, 9999, DeadlineInput{Kind: "other", DueDate: "2027-01-01"}, "manual", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deadline on missing race: %v, want ErrNotFound", err)
	}
}

func TestWatchNotesAreEncryptedAndPerUser(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	e, err := CreateEvent(ctx, d, validInput(), "", "manual", 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	w, err := SetWatch(ctx, d, 1, e.ID, WatchInput{State: "lottery_entered", Notes: "Applied with Kari"})
	if err != nil {
		t.Fatalf("set watch: %v", err)
	}
	if w.State != "lottery_entered" || w.Notes != "Applied with Kari" {
		t.Fatalf("watch = %+v", w)
	}
	var raw string
	if err := d.QueryRow(`SELECT notes FROM race_watch WHERE user_id = 1`).Scan(&raw); err != nil {
		t.Fatalf("read raw notes: %v", err)
	}
	if raw == "" || strings.Contains(raw, "Kari") {
		t.Fatalf("notes stored as %q, want ciphertext", raw)
	}
	if mine, _ := ListWatches(ctx, d, 2); len(mine) != 0 {
		t.Fatalf("user 2 sees %d watches, want 0", len(mine))
	}
	if _, err := SetWatch(ctx, d, 1, e.ID, WatchInput{State: "dreaming"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad state: %v, want validation error", err)
	}
	if _, err := SetWatch(ctx, d, 1, 9999, WatchInput{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("watch missing race: %v, want ErrNotFound", err)
	}
	if err := DeleteWatch(ctx, d, 1, e.ID); err != nil {
		t.Fatalf("delete watch: %v", err)
	}
	if err := DeleteWatch(ctx, d, 1, e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v, want ErrNotFound", err)
	}
}

func withUser(r *http.Request, id int64) *http.Request {
	return r.WithContext(auth.ContextWithUser(r.Context(), &auth.User{ID: id, Email: "a@example.com"}))
}

func withID(r *http.Request, name, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(name, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestHandlers(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	e, err := CreateEvent(ctx, d, validInput(), "", "manual", 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	idStr := strconv.FormatInt(e.ID, 10)

	// Watch via PUT, then the list shows it.
	body, _ := json.Marshal(WatchInput{State: "planning", Notes: "maybe"})
	req := withID(withUser(httptest.NewRequest(http.MethodPut, "/api/races/"+idStr+"/watch", bytes.NewReader(body)), 1), "id", idStr)
	rec := httptest.NewRecorder()
	HandleSetWatch(d)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("set watch: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	HandleList(d)(rec, withUser(httptest.NewRequest(http.MethodGet, "/api/races", nil), 1))
	var list struct {
		Events  []Event `json:"events"`
		Watches []Watch `json:"watches"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("list: %d %v", rec.Code, err)
	}
	if len(list.Events) != 1 || len(list.Watches) != 1 || list.Watches[0].State != "planning" {
		t.Fatalf("list = %+v", list)
	}

	// Detail includes the change history and the caller's watch only.
	rec = httptest.NewRecorder()
	HandleGet(d)(rec, withID(withUser(httptest.NewRequest(http.MethodGet, "/api/races/"+idStr, nil), 2), "id", idStr))
	var detail struct {
		Event   Event    `json:"event"`
		Changes []Change `json:"changes"`
		Watch   *Watch   `json:"watch"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("get: %d %v", rec.Code, err)
	}
	if detail.Watch != nil || len(detail.Changes) != 1 {
		t.Fatalf("detail for user 2 = watch %+v, %d changes", detail.Watch, len(detail.Changes))
	}

	// Invalid edits are 400 with the reason; missing races are 404.
	bad := validInput()
	bad.Status = "maybe"
	body, _ = json.Marshal(bad)
	rec = httptest.NewRecorder()
	HandleUpdate(d)(rec, withID(withUser(httptest.NewRequest(http.MethodPut, "/api/races/"+idStr, bytes.NewReader(body)), 1), "id", idStr))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "status") {
		t.Fatalf("bad update: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	HandleGet(d)(rec, withID(withUser(httptest.NewRequest(http.MethodGet, "/api/races/999", nil), 1), "id", "999"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing race: %d", rec.Code)
	}

	// Create via handler returns 201.
	body, _ = json.Marshal(validInput())
	rec = httptest.NewRecorder()
	HandleCreate(d)(rec, withUser(httptest.NewRequest(http.MethodPost, "/api/races", bytes.NewReader(body)), 1))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
}
