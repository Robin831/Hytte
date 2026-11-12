package doomsday

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Robin831/Hytte/internal/db"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Init(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'r@example.com', 'R', 'r')`); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestRecordChecksTheCalendar(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	// 17 May 1814 was a Tuesday (2). The client's own verdict is ignored.
	a, err := Record(ctx, d, 1, Attempt{Level: 4, Date: "1814-05-17", Answer: 2, ResponseMS: 9000, Correct: false})
	if err != nil || !a.Correct {
		t.Fatalf("right answer = %+v, %v", a, err)
	}
	a, err = Record(ctx, d, 1, Attempt{Level: 4, Date: "1814-05-17", Answer: 3, ResponseMS: 7000, Slip: "offByOne", Correct: true})
	if err != nil || a.Correct || a.Slip != "offByOne" {
		t.Fatalf("wrong answer = %+v, %v", a, err)
	}
	// Running out of time on the master level is a wrong answer.
	if a, err := Record(ctx, d, 1, Attempt{Level: 5, Date: "2026-01-01", Answer: -1, Slip: "timeout", ResponseMS: 30000}); err != nil || a.Correct {
		t.Fatalf("timeout = %+v, %v", a, err)
	}
	for _, bad := range []Attempt{
		{Level: 6, Date: "2026-01-01"},
		{Level: 3, Date: "2026-02-30"},
		{Level: 3, Date: "nope"},
		{Level: 3, Date: "2026-01-01", Answer: 7},
		{Level: 3, Date: "2026-01-01", Slip: "typo"},
		{Level: 2, Drill: "dance", Date: "2026-01-01"},
		{Level: 5, Date: "2026-01-01", Answer: -1},
		{Level: 5, Date: "2026-01-01", Answer: 3, Slip: "timeout"},
	} {
		if _, err := Record(ctx, d, 1, bad); !errors.Is(err, errInvalid) {
			t.Errorf("accepted %+v: %v", bad, err)
		}
	}
}

func TestStats(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	// Level 3: right (12 s), right (8 s, 1 hint), wrong (leap), right (10 s).
	for _, a := range []Attempt{
		{Level: 3, Date: "2026-11-12", Answer: 4, ResponseMS: 12000},
		{Level: 3, Date: "2026-11-12", Answer: 4, ResponseMS: 8000, Hints: 1},
		{Level: 3, Date: "2024-01-10", Answer: 4, ResponseMS: 9000, Slip: "leap"},
		{Level: 3, Date: "2026-11-12", Answer: 4, ResponseMS: 10000},
		{Level: 2, Drill: "month", Date: "2026-11-12", Answer: 0, ResponseMS: 2000, Correct: true},
	} {
		if _, err := Record(ctx, d, 1, a); err != nil {
			t.Fatal(err)
		}
	}
	st, err := LoadStats(ctx, d, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Levels) != 2 || st.Levels[0].Level != 2 || !(st.Levels[0].Correct == 1) {
		t.Fatalf("levels = %+v", st.Levels)
	}
	l3 := st.Levels[1]
	if l3.Attempts != 4 || l3.Correct != 3 || l3.BestMS != 10000 || l3.MedianMS != 10000 || l3.Streak != 1 || l3.BestStreak != 2 {
		t.Fatalf("level 3 = %+v", l3)
	}
	if st.Slips["leap"] != 1 || len(st.Recent) != 5 || st.Today["attempts"] != 5 || len(st.Daily) != 1 {
		t.Fatalf("stats = %+v", st)
	}
}
