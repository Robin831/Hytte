package trips

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/training"
)

func stubChat(d *sql.DB, reply func(prompt string) string) (*Chat, *string) {
	var seen string
	return &Chat{DB: d, Now: func() time.Time { return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC) },
		Config: func(context.Context, *sql.DB) (*training.ClaudeConfig, error) {
			return &training.ClaudeConfig{Enabled: true}, nil
		},
		Run: func(_ context.Context, _ *training.ClaudeConfig, p string) (string, float64, error) {
			seen = p
			return reply(p), 0.02, nil
		}}, &seen
}

func TestTripChatAnswersAndChangesTheTrip(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	id, _ := CreateTrip(ctx, d, 1, sampleInput())
	gid, _ := AddChecklist(ctx, d, id, 1, ChecklistInput{Title: I18n{"en": "Pack"}})
	item, _ := AddItem(ctx, d, gid, 1, ItemInput{Title: I18n{"en": "Passport"}})

	chat, prompt := stubChat(d, func(p string) string {
		var in TripInput
		// Claude echoes the trip with one change: a new note.
		start := strings.Index(p, "{\n \"kind\"")
		_ = json.NewDecoder(strings.NewReader(p[start:])).Decode(&in)
		in.Doc.Notes = append(in.Doc.Notes, Note{Title: I18n{"nb": "Kamp", "en": "Match", "th": "แข่ง"}, Body: I18n{"nb": "18:00", "en": "18:00", "th": "18:00"}})
		up, _ := json.Marshal(in)
		return "Added a note and ticked the passport.\n\n```json\n" + `{"update": ` + string(up) + `,
			"checklist": [{"op":"set_done","item_id":` + itoa(item) + `,"done":true},
			              {"op":"add_item","list_id":` + itoa(gid) + `,"title":{"nb":"Lader","en":"Charger","th":"ที่ชาร์จ"}}]}` + "\n```"
	})
	answer, err := chat.Ask(ctx, id, 1, "Add a note that kick-off is 18:00 and tick the passport")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chat.Ask(ctx, id, 1, "again"); !errors.Is(err, ErrChatBusy) {
		t.Fatalf("second question while answering: %v", err)
	}
	chat.Answer(ctx, id, 1, answer, "en")

	if !strings.Contains(*prompt, "YJFSBP") || !strings.Contains(*prompt, "Passport") || !strings.Contains(*prompt, "can change this trip") ||
		!strings.Contains(*prompt, "Reply in English") || !strings.Contains(*prompt, "kick-off is 18:00") {
		t.Fatalf("prompt lacks context:\n%s", *prompt)
	}
	msgs, _ := ListChat(ctx, d, id, 1)
	if len(msgs) != 2 || msgs[0].UserName != "Robin" || msgs[1].Status != "done" || !msgs[1].Changed || msgs[1].Content != "Added a note and ticked the passport." {
		t.Fatalf("messages = %+v", msgs)
	}
	got, _ := GetTrip(ctx, d, id, 1)
	if len(got.Doc.Notes) != 1 || got.Doc.Notes[0].Title["en"] != "Match" || got.Doc.Flights[0].BookingRef != "YJFSBP" {
		t.Fatalf("doc = %+v", got.Doc)
	}
	if items := got.Checklists[0].Items; len(items) != 2 || !items[0].Done || items[1].Title["en"] != "Charger" {
		t.Fatalf("checklist = %+v", items)
	}
	var raw string
	_ = d.QueryRow(`SELECT content FROM trip_chat_messages WHERE role = 'user'`).Scan(&raw)
	if strings.Contains(raw, "kick-off") {
		t.Fatal("chat messages must be encrypted")
	}
}

func TestTripChatViewerCannotChange(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	in := sampleInput()
	in.ShareFamily = true
	id, _ := CreateTrip(ctx, d, 1, in)
	chat, prompt := stubChat(d, func(string) string {
		return "ok\n```json\n" + `{"update":{"kind":"other","start_date":"2026-10-02","end_date":"2026-10-05","doc":{"title":{"en":"Hacked"}}},"checklist":[]}` + "\n```"
	})
	answer, err := chat.Ask(ctx, id, 3, "rename it") // user 3 can view (shared) but not edit
	if err != nil {
		t.Fatal(err)
	}
	chat.Answer(ctx, id, 3, answer, "nb")
	if !strings.Contains(*prompt, "can view this trip but not change it") {
		t.Fatal("viewer rights not in the prompt")
	}
	got, _ := GetTrip(ctx, d, id, 1)
	if got.Doc.Title["en"] != "Cardiff Race Weekend" {
		t.Fatal("a viewer's chat must not change the trip")
	}
	if msgs, _ := ListChat(ctx, d, id, 3); msgs[1].Changed {
		t.Fatal("answer marked as changed")
	}
	if err := ClearChat(ctx, d, id, 3); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer clearing chat: %v", err)
	}
	if _, err := chat.Ask(ctx, id, 2, "hi"); err != nil {
		t.Fatalf("shared trip, user 2 can ask: %v", err)
	}
}

func TestTripChatRefusesStaleChanges(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	id, _ := CreateTrip(ctx, d, 1, sampleInput())
	chat, _ := stubChat(d, func(string) string {
		// Someone edits the trip while Claude is thinking.
		_, _ = d.Exec(`UPDATE trips SET updated_at = 'later' WHERE id = ?`, id)
		return "Renamed.\n```json\n" + `{"update":{"kind":"race","start_date":"2026-10-02","end_date":"2026-10-05","doc":{"title":{"en":"New"}}},"checklist":[]}` + "\n```"
	})
	answer, _ := chat.Ask(ctx, id, 1, "rename")
	chat.Answer(ctx, id, 1, answer, "en")
	msgs, _ := ListChat(ctx, d, id, 1)
	if !strings.Contains(msgs[1].Content, "wasn't saved") || msgs[1].Changed {
		t.Fatalf("stale change = %+v", msgs[1])
	}
}

func TestTripChatCleanup(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	old := sampleInput()
	old.StartDate, old.EndDate = "2026-08-01", "2026-08-03"
	oldID, _ := CreateTrip(ctx, d, 1, old)
	newID, _ := CreateTrip(ctx, d, 1, sampleInput())
	uid := int64(1)
	for _, tid := range []int64{oldID, newID} {
		_, _ = insertChat(ctx, d, tid, &uid, "user", "hello", "done")
	}
	_, _ = insertChat(ctx, d, newID, nil, "assistant", "", "pending")
	n, err := CleanupChats(ctx, d, time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC), true)
	if err != nil || n != 1 {
		t.Fatalf("deleted %d, %v", n, err)
	}
	msgs, _ := ListChat(ctx, d, newID, 1)
	if len(msgs) != 2 || msgs[1].Status != "failed" {
		t.Fatalf("recent trip's chat = %+v", msgs)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
