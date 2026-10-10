package trips

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/training"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-trips-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })
	d, err := db.Init(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`INSERT INTO users (id, email, name, picture, google_id, created_at) VALUES
		(1, 'a@x', 'Robin Smith', '', 'g1', ''), (2, 'b@x', 'William Smith', '', 'g2', ''), (3, 'c@x', 'Kari', '', 'g3', '')`); err != nil {
		t.Fatal(err)
	}
	return d
}

func sampleInput() TripInput {
	return TripInput{
		Kind: "race", StartDate: "2026-10-02", EndDate: "2026-10-05", HomeTZ: "Europe/Oslo", DestTZ: "Europe/London",
		Doc: Doc{
			Title:      I18n{"en": "Cardiff Race Weekend", "nb": " Cardiff-helg "},
			Route:      []string{"bgo", "AMS", "CWL", "Cardiff centre"},
			Travellers: []Traveller{{Name: "Robin"}, {Name: "Khatiya"}},
			Flights: []Flight{{Airline: "KLM", FlightNo: "KL1169", From: "AMS", To: "BGO", DepLocal: "2026-10-05T15:10", DepTZ: "Europe/Amsterdam",
				ArrLocal: "2026-10-05T16:50", ArrTZ: "Europe/Oslo", BookingRef: "YJFSBP"}},
			Days: []Day{{Date: "2026-10-04", Title: I18n{"en": "Race day"}, Highlight: true,
				Steps: []Step{{Time: "07:00", Text: I18n{"en": "Breakfast", "nb": "Frokost", "th": "อาหารเช้า"}}}}},
		},
	}
}

func TestTripCRUDAccessAndEncryption(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	in := sampleInput()
	uid := int64(2)
	in.Doc.Travellers = append(in.Doc.Travellers, Traveller{Name: "William", UserID: &uid})
	id, err := CreateTrip(ctx, d, 1, in)
	if err != nil {
		t.Fatal(err)
	}

	var raw string
	_ = d.QueryRow(`SELECT doc FROM trips WHERE id = ?`, id).Scan(&raw)
	if strings.Contains(raw, "YJFSBP") || strings.Contains(raw, "Cardiff") {
		t.Fatal("trip document stored in plaintext")
	}

	got, err := GetTrip(ctx, d, id, 1)
	if err != nil || !got.CanEdit || got.Doc.Title["nb"] != "Cardiff-helg" {
		t.Fatalf("owner get = %+v, %v", got, err)
	}
	if strings.Join(got.Doc.Route, ",") != "BGO,AMS,CWL,Cardiff centre" {
		t.Fatalf("route = %v", got.Doc.Route)
	}
	if got.Doc.Flights[0].BookingRef != "YJFSBP" {
		t.Fatal("booking ref lost")
	}
	if g, err := GetTrip(ctx, d, id, 2); err != nil || !g.CanEdit {
		t.Fatalf("member get: %v", err)
	}
	if _, err := GetTrip(ctx, d, id, 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider get: %v", err)
	}
	if list, _ := ListTrips(ctx, d, 3); len(list) != 0 {
		t.Fatalf("outsider sees %d trips", len(list))
	}

	// Family sharing: outsiders can view, not edit.
	in.ShareFamily = true
	if err := UpdateTrip(ctx, d, id, 1, in); err != nil {
		t.Fatal(err)
	}
	if g, err := GetTrip(ctx, d, id, 3); err != nil || g.CanEdit {
		t.Fatalf("shared view = %+v, %v", g, err)
	}
	if err := UpdateTrip(ctx, d, id, 3, in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider edit: %v", err)
	}

	bad := sampleInput()
	bad.EndDate = "2026-09-01"
	if _, err := CreateTrip(ctx, d, 1, bad); !errors.Is(err, ErrValidation) {
		t.Fatalf("end before start: %v", err)
	}
	bad = sampleInput()
	bad.Doc.Flights[0].DepTZ = "Mars/Base"
	if _, err := CreateTrip(ctx, d, 1, bad); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad flight tz: %v", err)
	}

	if err := DeleteTrip(ctx, d, id, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("member delete: %v", err)
	}
	if err := DeleteTrip(ctx, d, id, 1); err != nil {
		t.Fatal(err)
	}
}

func TestChecklistsRecordWhoTicked(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	in := sampleInput()
	uid := int64(2)
	in.Doc.Travellers = append(in.Doc.Travellers, Traveller{Name: "William", UserID: &uid})
	id, _ := CreateTrip(ctx, d, 1, in)
	gid, err := AddChecklist(ctx, d, id, 1, ChecklistInput{Title: I18n{"en": "Pack"}})
	if err != nil {
		t.Fatal(err)
	}
	iid, err := AddItem(ctx, d, gid, 1, ItemInput{Title: I18n{"en": "Passport"}, Urgent: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := SetDone(ctx, d, iid, 2, true); err != nil {
		t.Fatal(err)
	}
	if err := SetDone(ctx, d, iid, 3, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider tick: %v", err)
	}
	got, _ := GetTrip(ctx, d, id, 1)
	item := got.Checklists[0].Items[0]
	if !item.Done || item.DoneByName != "William" || item.DoneAt == "" || !item.Urgent {
		t.Fatalf("item = %+v", item)
	}
	if list, _ := ListTrips(ctx, d, 1); list[0].Open != 0 || list[0].Total != 1 {
		t.Fatalf("summary counts = %+v", list[0])
	}
	if err := SetDone(ctx, d, iid, 1, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := GetTrip(ctx, d, id, 1); got.Checklists[0].Items[0].Done {
		t.Fatal("untick failed")
	}
	if _, err := AddItem(ctx, d, gid, 1, ItemInput{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty item: %v", err)
	}
}

func TestTranslationFillsMissingLanguages(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	id, _ := CreateTrip(ctx, d, 1, sampleInput())
	gid, _ := AddChecklist(ctx, d, id, 1, ChecklistInput{Title: I18n{"nb": "Pakking"}})
	_, _ = AddItem(ctx, d, gid, 1, ItemInput{Title: I18n{"en": "Passport"}})

	var prompt string
	tr := &Translator{DB: d,
		Config: func(context.Context, *sql.DB) (*training.ClaudeConfig, error) {
			return &training.ClaudeConfig{Enabled: true}, nil
		},
		Run: func(_ context.Context, _ *training.ClaudeConfig, p string) (string, float64, error) {
			prompt = p
			// Answer every gap: ids are numbered in walk order; fill generously.
			out := `{`
			for i := 1; i <= 10; i++ {
				if i > 1 {
					out += ","
				}
				out += `"` + string(rune('0'+i%10)) + `": {"nb": "NB", "en": "EN", "th": "TH"}`
			}
			return out + `}`, 0.01, nil
		}}
	tr.TranslateTrip(ctx, id, 1)

	if !strings.Contains(prompt, "Cardiff Race Weekend") || strings.Contains(prompt, "อาหารเช้า") {
		t.Fatalf("prompt should include gaps only:\n%s", prompt)
	}
	got, _ := GetTrip(ctx, d, id, 1)
	if got.Doc.Title["th"] == "" || got.Doc.Title["en"] != "Cardiff Race Weekend" {
		t.Fatalf("title = %v", got.Doc.Title)
	}
	if got.Doc.Days[0].Steps[0].Text["th"] != "อาหารเช้า" {
		t.Fatalf("complete text was changed: %v", got.Doc.Days[0].Steps[0].Text)
	}
	if got.Checklists[0].Title["en"] == "" || got.Checklists[0].Items[0].Title["th"] == "" {
		t.Fatalf("checklist texts = %v / %v", got.Checklists[0].Title, got.Checklists[0].Items[0].Title)
	}
}

func TestImportMapsTravellersAndLinksResult(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	if _, err := d.Exec(`INSERT INTO race_results (user_id, race_name, race_date, distance_m, created_at, updated_at)
		VALUES (1, 'Cardiff Half', '2026-10-04', 21097, '', '')`); err != nil {
		t.Fatal(err)
	}
	in := sampleInput()
	f := ImportFile{Doc: in.Doc, Kind: in.Kind, StartDate: in.StartDate, EndDate: in.EndDate, HomeTZ: in.HomeTZ, DestTZ: in.DestTZ}
	f.Race = &Race{Name: "Cardiff Half", Date: "2026-10-04", Result: "1:51:08"}
	f.Travellers = append(f.Travellers, Traveller{Name: "William"})
	f.Checklists = append(f.Checklists, struct {
		Phase string `json:"phase"`
		Title I18n   `json:"title"`
		Items []struct {
			Title  I18n `json:"title"`
			Detail I18n `json:"detail"`
			Done   bool `json:"done"`
			Urgent bool `json:"urgent"`
		} `json:"items"`
	}{Title: I18n{"en": "Saturday"}})
	f.Checklists[0].Items = append(f.Checklists[0].Items, struct {
		Title  I18n `json:"title"`
		Detail I18n `json:"detail"`
		Done   bool `json:"done"`
		Urgent bool `json:"urgent"`
	}{Title: I18n{"en": "Collect bib"}, Done: true})

	id, err := ImportTrip(ctx, d, 1, f)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := GetTrip(ctx, d, id, 1)
	if got.ResultID == nil || len(got.Checklists) != 1 || !got.Checklists[0].Items[0].Done {
		t.Fatalf("imported = %+v", got)
	}
	if g, err := GetTrip(ctx, d, id, 2); err != nil || !g.CanEdit {
		t.Fatalf("William (a Hytte user) should be a member: %v", err)
	}
}
