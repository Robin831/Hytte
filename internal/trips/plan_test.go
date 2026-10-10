package trips

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/encryption"
)

func TestFamilyRoster(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	people, err := ListPeople(ctx, d)
	if err != nil || len(people) != 3 || people[0].Name != "Robin" || people[1].Name != "William" || people[0].UserID == nil {
		t.Fatalf("users on the roster = %+v, %v", people, err)
	}
	by := 1981
	id, err := AddPerson(ctx, d, PersonInput{Name: " Khatiya ", BirthYear: &by})
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	_ = d.QueryRow(`SELECT name FROM family_people WHERE id = ?`, id).Scan(&raw)
	if strings.Contains(raw, "Khatiya") || encryption.DecryptLenient(raw) != "Khatiya" {
		t.Fatal("roster names must be encrypted")
	}
	if _, err := AddPerson(ctx, d, PersonInput{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("nameless person: %v", err)
	}
	bad := 1850
	if _, err := AddPerson(ctx, d, PersonInput{Name: "X", BirthYear: &bad}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad birth year: %v", err)
	}

	// A Hytte user can get a birth year and a display name, and go back.
	wy := 2017
	if err := UpdatePerson(ctx, d, people[1].ID, PersonInput{Name: "Will", BirthYear: &wy}); err != nil {
		t.Fatal(err)
	}
	if err := UpdatePerson(ctx, d, people[1].ID, PersonInput{BirthYear: &wy}); err != nil {
		t.Fatal(err)
	}
	people, _ = ListPeople(ctx, d)
	if people[1].Name != "William" || people[1].BirthYear == nil || *people[1].BirthYear != 2017 || people[3].Name != "Khatiya" {
		t.Fatalf("roster = %+v", people)
	}
	if err := DeletePerson(ctx, d, people[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("users can't be removed: %v", err)
	}
	if err := DeletePerson(ctx, d, id); err != nil {
		t.Fatal(err)
	}
}

func TestTravellersFromRoster(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	people, _ := ListPeople(ctx, d)
	wy := 2017
	_ = UpdatePerson(ctx, d, people[1].ID, PersonInput{BirthYear: &wy})
	oy := 2024
	olivia, _ := AddPerson(ctx, d, PersonInput{Name: "Olivia", BirthYear: &oy})

	in := sampleInput()
	in.Doc.Travellers = []Traveller{{PersonID: &people[0].ID}, {PersonID: &people[1].ID}, {PersonID: &olivia}, {Name: "Granny"}}
	id, err := CreateTrip(ctx, d, 1, in)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := GetTrip(ctx, d, id, 1)
	tr := got.Doc.Travellers
	if tr[0].Name != "Robin" || tr[1].Name != "William" || !tr[1].Child || tr[1].UserID == nil || tr[2].Name != "Olivia" || !tr[2].Child || tr[2].UserID != nil {
		t.Fatalf("travellers = %+v", tr)
	}
	if g, err := GetTrip(ctx, d, id, 2); err != nil || !g.CanEdit {
		t.Fatalf("William (picked from the roster) should be a member: %v", err)
	}
	unknown := int64(999)
	in.Doc.Travellers = []Traveller{{PersonID: &unknown}}
	if err := UpdateTrip(ctx, d, id, 1, in); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown traveller: %v", err)
	}
}

func TestFlexibleDatesAndClashes(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()

	in := sampleInput()
	in.Kind = "holiday"
	in.Doc.Flights, in.Doc.Days = nil, nil
	in.Doc.Flex = &Flex{From: "2026-11-01", To: "2026-11-30", Nights: 2, DepartDays: []int{5, 5}}
	id, err := CreateTrip(ctx, d, 1, in)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := GetTrip(ctx, d, id, 1)
	if got.StartDate != "2026-11-01" || got.EndDate != "2026-11-30" || len(got.Doc.Flex.DepartDays) != 1 {
		t.Fatalf("tentative trip spans its window: %+v / %+v", got.StartDate, got.Doc.Flex)
	}
	if list, _ := ListTrips(ctx, d, 1); !list[0].Tentative {
		t.Fatal("listed trip should be tentative")
	}

	// Clashes: an all-day calendar event on Sat 14 Nov, a registered race on
	// Sun 22 Nov, and another (fixed) trip 26–29 Nov.
	encTitle, _ := encryption.EncryptField("Cabin weekend")
	if _, err := d.Exec(`INSERT INTO calendar_events (id, user_id, calendar_id, title, start_time, end_time, all_day, status)
		VALUES ('e1', 1, 'primary', ?, '2026-11-14T00:00:00Z', '2026-11-15T00:00:00Z', 1, 'confirmed')`, encTitle); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO race_events (id, slug, name, race_date, distance_m) VALUES (7, 'x-2026', 'Lisbon Half', '2026-11-22', 21097)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO race_watch (user_id, event_id, state, created_at, updated_at) VALUES (1, 7, 'registered', '', '')`); err != nil {
		t.Fatal(err)
	}
	other := sampleInput()
	other.StartDate, other.EndDate = "2026-11-26", "2026-11-29"
	if _, err := CreateTrip(ctx, d, 1, other); err != nil {
		t.Fatal(err)
	}

	cands, err := Candidates(ctx, d, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Fridays in Nov 2026 with 2 nights inside the window: 6, 13, 20, 27.
	if len(cands) != 4 || cands[0].Start != "2026-11-06" || cands[0].End != "2026-11-08" || cands[3].Start != "2026-11-27" {
		t.Fatalf("candidates = %+v", cands)
	}
	if len(cands[0].Clashes) != 0 {
		t.Fatalf("6 Nov should be free: %+v", cands[0].Clashes)
	}
	if c := cands[1].Clashes; len(c) != 1 || c[0].Kind != "calendar" || c[0].Title != "Cabin weekend" || c[0].Who != "Robin" {
		t.Fatalf("13 Nov clashes = %+v", c)
	}
	if c := cands[2].Clashes; len(c) != 1 || c[0].Kind != "race" || c[0].ID != 7 {
		t.Fatalf("20 Nov clashes = %+v", c)
	}
	if c := cands[3].Clashes; len(c) != 1 || c[0].Kind != "trip" || c[0].TitleI18n["en"] != "Cardiff Race Weekend" {
		t.Fatalf("27 Nov clashes = %+v", c)
	}

	// Choosing a candidate fixes the dates and keeps the window.
	in.Doc.Flex.Chosen = true
	in.StartDate, in.EndDate = "2026-11-06", "2026-11-08"
	if err := UpdateTrip(ctx, d, id, 1, in); err != nil {
		t.Fatal(err)
	}
	got, _ = GetTrip(ctx, d, id, 1)
	if got.StartDate != "2026-11-06" || got.Doc.Flex == nil || got.Doc.Flex.From != "2026-11-01" {
		t.Fatalf("chosen = %s %+v", got.StartDate, got.Doc.Flex)
	}

	in.Doc.Flex = &Flex{From: "2026-11-01", To: "2026-11-02", Nights: 3}
	if err := UpdateTrip(ctx, d, id, 1, in); !errors.Is(err, ErrValidation) {
		t.Fatalf("window shorter than the stay: %v", err)
	}
}
