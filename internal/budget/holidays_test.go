package budget

import (
	"testing"
	"time"
)

func TestEasterSunday(t *testing.T) {
	// Known Easter Sunday dates for verification.
	cases := []struct {
		year  int
		month time.Month
		day   int
	}{
		{2024, time.March, 31},
		{2025, time.April, 20},
		{2026, time.April, 5},
		{2027, time.March, 28},
	}
	for _, tc := range cases {
		got := easterSunday(tc.year)
		want := time.Date(tc.year, tc.month, tc.day, 0, 0, 0, 0, time.UTC)
		if !got.Equal(want) {
			t.Errorf("easterSunday(%d) = %s, want %s", tc.year, got.Format("2006-01-02"), want.Format("2006-01-02"))
		}
	}
}

func TestNorwegianHolidays(t *testing.T) {
	h := norwegianHolidays(2026)

	mustBeHoliday := []string{
		"2026-01-01", // Nyttårsdag
		"2026-05-01", // Arbeidernes dag
		"2026-05-17", // Grunnlovsdagen
		"2026-12-25", // Første juledag
		"2026-12-26", // Andre juledag
		// Easter 2026 is April 5
		"2026-04-02", // Skjærtorsdag
		"2026-04-03", // Langfredag
		"2026-04-05", // Første påskedag
		"2026-04-06", // Andre påskedag
		"2026-05-14", // Kristi himmelfartsdag (April 5 + 39)
		"2026-05-24", // Første pinsedag (April 5 + 49)
		"2026-05-25", // Andre pinsedag (April 5 + 50)
	}
	for _, d := range mustBeHoliday {
		if !h[d] {
			t.Errorf("expected %s to be a Norwegian holiday in 2026", d)
		}
	}

	mustNotBeHoliday := []string{
		"2026-04-07", // Tuesday after Easter — not a holiday
		"2026-03-17", // Random day
	}
	for _, d := range mustNotBeHoliday {
		if h[d] {
			t.Errorf("expected %s to NOT be a Norwegian holiday in 2026", d)
		}
	}
}

func TestPreviousBusinessDay(t *testing.T) {
	cases := []struct {
		input string
		want  string
		desc  string
	}{
		// Weekday with no holiday — same day
		{"2026-04-07", "2026-04-07", "Tuesday stays"},
		// Saturday → previous Friday
		{"2026-04-11", "2026-04-10", "Saturday → Friday"},
		// Sunday → previous Friday
		{"2026-04-12", "2026-04-10", "Sunday → Friday"},
		// Easter Sunday 2026 (Apr 5) → preceding Wednesday (Apr 1), since Apr 2-4 are holidays
		{"2026-04-05", "2026-04-01", "Easter Sunday → Wednesday (Maundy Thu/Good Fri/Easter Sat are not working days)"},
		// Easter Monday 2026 (Apr 6) → preceding Wednesday (Apr 1)
		{"2026-04-06", "2026-04-01", "Easter Monday → Wednesday before Easter"},
		// Good Friday 2026 (Apr 3) → Wednesday Apr 1
		{"2026-04-03", "2026-04-01", "Good Friday → Wednesday"},
		// May 17 2026 is a Sunday → previous Friday May 15
		{"2026-05-17", "2026-05-15", "Constitution Day on Sunday → Friday"},
		// New Year's 2026 (Jan 1, Thursday) → previous Wednesday Dec 31, 2025
		{"2026-01-01", "2025-12-31", "New Year's Day → previous Wednesday"},
	}
	for _, tc := range cases {
		input, _ := time.Parse("2006-01-02", tc.input)
		got := previousBusinessDay(input)
		gotStr := got.Format("2006-01-02")
		if gotStr != tc.want {
			t.Errorf("%s: previousBusinessDay(%s) = %s, want %s", tc.desc, tc.input, gotStr, tc.want)
		}
	}
}

func TestNextBusinessDay(t *testing.T) {
	cases := []struct {
		input string
		want  string
		desc  string
	}{
		// Weekday with no holiday — same day
		{"2026-04-07", "2026-04-07", "Tuesday stays"},
		// Saturday → Tuesday after Easter Monday holiday
		{"2026-04-04", "2026-04-07", "Saturday → Tuesday after Easter Monday holiday"},
		// Sunday → Monday
		{"2026-04-12", "2026-04-13", "Sunday → Monday"},
		// Good Friday (holiday) — Easter 2026: Apr 3 (Langfredag), Apr 5 (Easter), Apr 6 (Easter Monday)
		{"2026-04-03", "2026-04-07", "Good Friday → next Tuesday (Mon Apr 6 is Easter Monday)"},
		// Langfredag falls on the 3rd, Easter Monday on 6th → next is 7th
		{"2026-04-06", "2026-04-07", "Easter Monday → Tuesday"},
		// Christmas 2026: Dec 25 (Friday), Dec 26 (Saturday holiday) → Dec 28 (Monday)
		{"2026-12-25", "2026-12-28", "Christmas Day (Friday) → Dec 28 (Monday, Dec 26 is holiday on Saturday)"},
		// New Year's 2026: Jan 1 is Thursday
		{"2026-01-01", "2026-01-02", "New Year's Day (Thursday) → Friday Jan 2"},
	}
	for _, tc := range cases {
		input, _ := time.Parse("2006-01-02", tc.input)
		got := nextBusinessDay(input)
		gotStr := got.Format("2006-01-02")
		if gotStr != tc.want {
			t.Errorf("%s: nextBusinessDay(%s) = %s, want %s", tc.desc, tc.input, gotStr, tc.want)
		}
	}
}
