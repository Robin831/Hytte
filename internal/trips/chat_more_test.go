package trips

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/training"
)

// A 1×1 PNG.
const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func TestTripChatReadsPicturesAndRemembers(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	in := sampleInput()
	in.StartDate, in.EndDate = "2026-10-06", "2026-10-16"
	in.Doc.Phases = []Phase{{Key: "surin", Title: I18n{"en": "Surin"}, Start: "2026-10-08", End: "2026-10-14"}}
	id, err := CreateTrip(ctx, d, 1, in)
	if err != nil {
		t.Fatal(err)
	}
	_ = AddNote(ctx, d, 1, "Khatiya is also called Tan")

	chat, prompt := stubChat(d, func(string) string {
		return "Noted the hotel.\n```json\n" + `{"update": null, "checklist": [], "remember": ["In Thailand they drive with family", "khatiya is also called tan"], "forget": []}` + "\n```"
	})
	var described []string
	chat.Describe = func(_ context.Context, _ *training.ClaudeConfig, p, path string) (string, error) {
		b, err := os.ReadFile(path)
		if err != nil || len(b) == 0 || !strings.HasSuffix(path, ".png") {
			t.Errorf("picture not on disk for the vision call: %v", err)
		}
		described = append(described, path)
		return "Sign: GO Hotel Bangkok Suvarnabhumi Airport", nil
	}

	answer, err := chat.Ask(ctx, id, 1, "", []ImageInput{{Data: "data:image/png;base64," + tinyPNG}})
	if err != nil {
		t.Fatal(err)
	}
	chat.Answer(ctx, id, 1, answer, "en")

	if len(described) != 1 {
		t.Fatalf("vision calls = %d", len(described))
	}
	if _, err := os.Stat(described[0]); !os.IsNotExist(err) {
		t.Fatal("the picture's temp directory must be removed after the vision call")
	}
	p := *prompt
	for _, want := range []string{
		"[picture: Sign: GO Hotel Bangkok Suvarnabhumi Airport]", // the picture reaches the assistant as text
		"is day 6 of 11", "Current phase: Surin", // 11 Oct in a 6–16 Oct trip
		"Khatiya is also called Tan", // memory
		"How Hytte trips work", "Act, don't interrogate", "hotel_query",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}

	msgs, _ := ListChat(ctx, d, id, 1)
	if len(msgs) != 2 || len(msgs[0].Images) != 1 || msgs[0].Content != "📷" || msgs[1].Content != "Noted the hotel." {
		t.Fatalf("messages = %+v", msgs)
	}
	mime, data, err := ChatImage(ctx, d, id, msgs[0].Images[0], 1)
	if err != nil || mime != "image/png" || base64.StdEncoding.EncodeToString(data) != tinyPNG {
		t.Fatalf("stored picture = %s, %d bytes, %v", mime, len(data), err)
	}
	if _, _, err := ChatImage(ctx, d, id, msgs[0].Images[0], 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an outsider must not get the picture: %v", err)
	}
	var raw string
	_ = d.QueryRow(`SELECT data FROM trip_chat_images`).Scan(&raw)
	if strings.Contains(raw, tinyPNG[:40]) {
		t.Fatal("pictures must be encrypted at rest")
	}

	// The new fact is remembered once (the duplicate, in other capitals, is not).
	notes, _ := ListNotes(ctx, d)
	if len(notes) != 2 || notes[1].Text != "In Thailand they drive with family" {
		t.Fatalf("notes = %+v", notes)
	}
	_ = d.QueryRow(`SELECT note FROM trip_assistant_notes LIMIT 1`).Scan(&raw)
	if strings.Contains(raw, "Khatiya") {
		t.Fatal("notes must be encrypted at rest")
	}

	// A second question doesn't read the old picture again, and can forget.
	chat.Run = func(_ context.Context, _ *training.ClaudeConfig, p string) (string, float64, error) {
		*prompt = p
		return "Forgot it.\n```json\n" + `{"forget": [` + itoa(notes[0].ID) + `]}` + "\n```", 0, nil
	}
	answer, _ = chat.Ask(ctx, id, 1, "forget her nickname", nil)
	chat.Answer(ctx, id, 1, answer, "en")
	if len(described) != 1 {
		t.Fatal("an already-read picture was read again")
	}
	if !strings.Contains(*prompt, "[picture: Sign: GO Hotel") {
		t.Fatal("earlier pictures should stay in the conversation as text")
	}
	if notes, _ = ListNotes(ctx, d); len(notes) != 1 {
		t.Fatalf("after forget = %+v", notes)
	}

	// Bad pictures are refused.
	if _, err := chat.Ask(ctx, id, 1, "x", []ImageInput{{Data: base64.StdEncoding.EncodeToString([]byte("not a picture"))}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("non-image: %v", err)
	}
	many := make([]ImageInput, 5)
	for i := range many {
		many[i].Data = tinyPNG
	}
	if _, err := chat.Ask(ctx, id, 1, "x", many); !errors.Is(err, ErrValidation) {
		t.Fatalf("too many pictures: %v", err)
	}
}

func TestTripStatus(t *testing.T) {
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	trip := &Trip{StartDate: "2026-11-14", EndDate: "2026-11-15", DestTZ: "Europe/Oslo"}
	if s := tripStatus(trip, now); !strings.Contains(s, "begins in 34 days") {
		t.Fatalf("before = %q", s)
	}
	trip = &Trip{StartDate: "2026-10-02", EndDate: "2026-10-05", DestTZ: "Europe/London"}
	if s := tripStatus(trip, now); !strings.Contains(s, "ended 6 days ago") {
		t.Fatalf("after = %q", s)
	}
	trip = &Trip{StartDate: "2026-11-01", EndDate: "2026-11-30", DestTZ: "Europe/Paris", Doc: Doc{Flex: &Flex{From: "2026-11-01", To: "2026-11-30", Nights: 2}}}
	if s := tripStatus(trip, now); !strings.Contains(s, "tentative") {
		t.Fatalf("tentative = %q", s)
	}
}

func TestNamedHotelSearch(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	in := sampleInput() // no destination at all
	in.StartDate, in.EndDate = "2026-10-06", "2026-10-16"
	id, _ := CreateTrip(ctx, d, 1, in)

	var calls []map[string]string
	single := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := map[string]string{}
		for k := range r.URL.Query() {
			q[k] = r.URL.Query().Get(k)
		}
		calls = append(calls, q)
		if single {
			// Google resolved the search to one exact hotel.
			_, _ = w.Write([]byte(`{"name":"GO Hotel Bangkok Suvarnabhumi Airport","overall_rating":4.1,"link":"https://gohotel.example",
				"rate_per_night":{"extracted_lowest":321},"total_rate":{"extracted_lowest":321},
				"prices":[{"source":"Agoda","link":"https://agoda.example","rate_per_night":{"extracted_lowest":305},"total_rate":{"extracted_lowest":305}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"properties":[
			{"name":"Palette Bangkok Airport Hotel","overall_rating":4.2,"total_rate":{"extracted_lowest":129},"rate_per_night":{"extracted_lowest":129}},
			{"name":"GO Hotel Bangkok Suvarnabhumi Airport","overall_rating":4.1,"total_rate":{"extracted_lowest":321},"rate_per_night":{"extracted_lowest":321}}]}`))
	}))
	t.Cleanup(srv.Close)
	s := &Searcher{DB: d, HTTP: srv.Client(), Base: srv.URL, Key: "k", Limit: 20, Now: func() time.Time { return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC) }}

	opts := SearchOptions{Dates: []DatePair{{"2026-10-14", "2026-10-15"}}, HotelQuery: "GO Hotel Bangkok Suvarnabhumi Airport"}
	n, err := s.Start(ctx, id, 1, opts)
	if err != nil || n != 1 {
		t.Fatalf("queued %d, %v (a named hotel needs one hotel search and no flights)", n, err)
	}
	v := waitDone(t, s, id)
	if len(calls) != 1 || calls[0]["engine"] != "google_hotels" || calls[0]["q"] != "GO Hotel Bangkok Suvarnabhumi Airport" {
		t.Fatalf("calls = %+v", calls)
	}
	q := v.Quotes[0]
	var ho []HotelOption
	_ = json.Unmarshal(q.Options, &ho)
	if q.Label != "GO Hotel Bangkok Suvarnabhumi Airport" || len(ho) != 2 || !strings.HasPrefix(ho[0].Name, "GO Hotel") || *q.Cheapest != 321 {
		t.Fatalf("named hotel should lead and set the price: %+v / %+v", q, ho)
	}

	// Google's single-hotel page is read too.
	single = true
	opts.Dates = []DatePair{{"2026-10-15", "2026-10-16"}}
	if n, err := s.Start(ctx, id, 1, opts); err != nil || n != 1 {
		t.Fatalf("second search: %d, %v", n, err)
	}
	v = waitDone(t, s, id)
	q = v.Quotes[len(v.Quotes)-1]
	_ = json.Unmarshal(q.Options, &ho)
	// The cheapest booking site first, then the hotel's own price.
	if len(ho) != 2 || ho[0].Name != "GO Hotel Bangkok Suvarnabhumi Airport · Agoda" || ho[0].Total != 305 || ho[1].Total != 321 || *q.Cheapest != 305 {
		t.Fatalf("single-hotel reply = %+v", ho)
	}

	// Flights with no airports and no hotel text: nothing to search.
	if _, err := s.Start(ctx, id, 1, SearchOptions{Kinds: []string{"flights"}}); !errors.Is(err, ErrNoDestination) {
		t.Fatalf("flights without airports: %v", err)
	}
	if _, err := s.Start(ctx, id, 1, SearchOptions{Kinds: []string{"cars"}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown kind: %v", err)
	}
}
