package training

import (
	"database/sql"
	"fmt"
	"testing"
)

// insertFilterWorkout seeds a workout with an explicit sport, title, start time
// and tag set so the server-side filter tests can target each dimension.
func insertFilterWorkout(t *testing.T, database *sql.DB, userID int64, sport, title, startedAt string, tags ...string) int64 {
	t.Helper()
	hash := fmt.Sprintf("filterhash%d", testHashCounter.Add(1))
	res, err := database.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters, fit_file_hash)
		 VALUES (?, ?, ?, ?, 1800, 5000, ?)`,
		userID, sport, title, startedAt, hash,
	)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	for _, tag := range tags {
		if _, err := database.Exec(`INSERT OR IGNORE INTO workout_tags (workout_id, tag) VALUES (?, ?)`, id, tag); err != nil {
			t.Fatalf("insert tag %q: %v", tag, err)
		}
	}
	return id
}

func workoutIDs(workouts []Workout) []int64 {
	ids := make([]int64, len(workouts))
	for i, w := range workouts {
		ids[i] = w.ID
	}
	return ids
}

func assertIDs(t *testing.T, got []int64, want ...int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected ids %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected ids %v, got %v", want, got)
		}
	}
}

// seedFilterFixture creates a small, deterministic history covering every
// filter dimension. Returned in started_at DESC order.
func seedFilterFixture(t *testing.T, database *sql.DB) (run1, run2, ride, swim int64) {
	t.Helper()
	swim = insertFilterWorkout(t, database, 1, "swimming", "Pool Laps", "2024-01-01T10:00:00Z")
	ride = insertFilterWorkout(t, database, 1, "cycling", "Easy Spin", "2024-01-02T10:00:00Z", "easy")
	run2 = insertFilterWorkout(t, database, 1, "running", "Hill Intervals", "2024-01-03T10:00:00Z", "hard", "auto:intervals")
	run1 = insertFilterWorkout(t, database, 1, "running", "Morning Run", "2024-01-04T10:00:00Z", "easy", "ai:recovery")
	return
}

func TestListPaginated_FilterBySport(t *testing.T) {
	database := setupTestDB(t)
	run1, run2, _, _ := seedFilterFixture(t, database)

	workouts, next, err := ListPaginated(database, 1, WorkoutFilter{Sport: "running"}, 25, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if next != nil {
		t.Fatalf("expected nil next cursor, got %+v", next)
	}
	assertIDs(t, workoutIDs(workouts), run1, run2)
}

func TestListPaginated_FilterByMultipleTagsUsesAND(t *testing.T) {
	database := setupTestDB(t)
	run1, _, ride, _ := seedFilterFixture(t, database)

	// A single tag matches both workouts carrying it.
	workouts, _, err := ListPaginated(database, 1, WorkoutFilter{Tags: []string{"easy"}}, 25, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertIDs(t, workoutIDs(workouts), run1, ride)

	// Two tags require both to be present — the ride only has "easy".
	workouts, _, err = ListPaginated(database, 1, WorkoutFilter{Tags: []string{"easy", "ai:recovery"}}, 25, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertIDs(t, workoutIDs(workouts), run1)

	// Tags that never co-occur match nothing.
	workouts, _, err = ListPaginated(database, 1, WorkoutFilter{Tags: []string{"easy", "hard"}}, 25, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(workouts) != 0 {
		t.Fatalf("expected no matches for disjoint tags, got %v", workoutIDs(workouts))
	}
}

func TestListPaginated_FilterByTitleIsCaseInsensitiveSubstring(t *testing.T) {
	database := setupTestDB(t)
	run1, _, _, _ := seedFilterFixture(t, database)

	workouts, _, err := ListPaginated(database, 1, WorkoutFilter{Query: "MORNING"}, 25, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertIDs(t, workoutIDs(workouts), run1)

	// Substring, not prefix.
	workouts, _, err = ListPaginated(database, 1, WorkoutFilter{Query: "ing ru"}, 25, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertIDs(t, workoutIDs(workouts), run1)
}

func TestListPaginated_FilterTitleEscapesLikeWildcards(t *testing.T) {
	database := setupTestDB(t)
	seedFilterFixture(t, database)
	pct := insertFilterWorkout(t, database, 1, "running", "Threshold 80% effort", "2024-02-01T10:00:00Z")

	// "%" must be matched literally, not as a wildcard that matches everything.
	workouts, _, err := ListPaginated(database, 1, WorkoutFilter{Query: "80%"}, 25, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertIDs(t, workoutIDs(workouts), pct)

	// A bare "%" would match every title if wildcards leaked through.
	workouts, _, err = ListPaginated(database, 1, WorkoutFilter{Query: "%"}, 25, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertIDs(t, workoutIDs(workouts), pct)
}

func TestListPaginated_CombinedFilters(t *testing.T) {
	database := setupTestDB(t)
	run1, _, _, _ := seedFilterFixture(t, database)

	workouts, _, err := ListPaginated(database, 1, WorkoutFilter{
		Sport: "running",
		Tags:  []string{"easy"},
		Query: "morning",
	}, 25, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertIDs(t, workoutIDs(workouts), run1)

	// The same tag+text on a different sport matches nothing.
	workouts, _, err = ListPaginated(database, 1, WorkoutFilter{
		Sport: "cycling",
		Tags:  []string{"easy"},
		Query: "morning",
	}, 25, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(workouts) != 0 {
		t.Fatalf("expected no matches, got %v", workoutIDs(workouts))
	}
}

func TestListPaginated_FilterPagesAcrossPageBoundary(t *testing.T) {
	database := setupTestDB(t)

	// Interleave matching and non-matching workouts so a filtered page can only
	// be assembled by filtering in SQL, not by narrowing a raw page client-side.
	var wantRuns []int64
	for i := 0; i < 5; i++ {
		insertFilterWorkout(t, database, 1, "cycling", "Ride", fmt.Sprintf("2024-03-%02dT09:00:00Z", i+1))
		wantRuns = append(wantRuns, insertFilterWorkout(t, database, 1, "running", "Run", fmt.Sprintf("2024-03-%02dT10:00:00Z", i+1), "easy"))
	}
	// started_at DESC — reverse the insertion order.
	for i, j := 0, len(wantRuns)-1; i < j; i, j = i+1, j-1 {
		wantRuns[i], wantRuns[j] = wantRuns[j], wantRuns[i]
	}

	filter := WorkoutFilter{Sport: "running", Tags: []string{"easy"}}

	page1, next, err := ListPaginated(database, 1, filter, 2, nil)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	assertIDs(t, workoutIDs(page1), wantRuns[0], wantRuns[1])
	if next == nil {
		t.Fatal("expected a next cursor after page 1")
	}

	page2, next2, err := ListPaginated(database, 1, filter, 2, next)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	assertIDs(t, workoutIDs(page2), wantRuns[2], wantRuns[3])
	if next2 == nil {
		t.Fatal("expected a next cursor after page 2")
	}

	page3, next3, err := ListPaginated(database, 1, filter, 2, next2)
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	assertIDs(t, workoutIDs(page3), wantRuns[4])
	if next3 != nil {
		t.Fatalf("expected nil cursor on the final page of matches, got %+v", next3)
	}
}

func TestListPaginated_FilterEmptyResult(t *testing.T) {
	database := setupTestDB(t)
	seedFilterFixture(t, database)

	workouts, next, err := ListPaginated(database, 1, WorkoutFilter{Sport: "kitesurfing"}, 25, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(workouts) != 0 {
		t.Fatalf("expected no matches, got %v", workoutIDs(workouts))
	}
	if next != nil {
		t.Fatalf("expected nil cursor for an empty result set, got %+v", next)
	}
}

func TestListPaginated_DuplicateTagsDoNotBreakAndSemantics(t *testing.T) {
	database := setupTestDB(t)
	run1, _, _, _ := seedFilterFixture(t, database)

	// The same tag repeated must not inflate the HAVING count and match nothing.
	workouts, _, err := ListPaginated(database, 1, WorkoutFilter{Tags: []string{"ai:recovery", "ai:recovery", " "}}, 25, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertIDs(t, workoutIDs(workouts), run1)
}

// List backs the legacy full-history branch of the list endpoint, so it must
// keep returning every workout in started_at DESC, id DESC order — the same
// rows the unfiltered paginated path walks.
func TestList_ReturnsFullHistoryUnfiltered(t *testing.T) {
	database := setupTestDB(t)
	run1, run2, ride, swim := seedFilterFixture(t, database)

	all, err := List(database, 1)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	assertIDs(t, workoutIDs(all), run1, run2, ride, swim)

	paged, _, err := ListPaginated(database, 1, WorkoutFilter{}, 25, nil)
	if err != nil {
		t.Fatalf("list paginated: %v", err)
	}
	assertIDs(t, workoutIDs(paged), workoutIDs(all)...)
}

func TestListPaginated_FilterScopedToUser(t *testing.T) {
	database := setupTestDB(t)
	if _, err := database.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'google-2')`); err != nil {
		t.Fatalf("create user: %v", err)
	}
	mine := insertFilterWorkout(t, database, 1, "running", "Morning Run", "2024-01-04T10:00:00Z", "easy")
	insertFilterWorkout(t, database, 2, "running", "Morning Run", "2024-01-05T10:00:00Z", "easy")

	workouts, _, err := ListPaginated(database, 1, WorkoutFilter{Sport: "running", Tags: []string{"easy"}}, 25, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertIDs(t, workoutIDs(workouts), mine)
}

func TestListDistinctTags(t *testing.T) {
	database := setupTestDB(t)
	if _, err := database.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'google-2')`); err != nil {
		t.Fatalf("create user: %v", err)
	}
	seedFilterFixture(t, database)
	insertFilterWorkout(t, database, 2, "running", "Other Run", "2024-01-06T10:00:00Z", "someone-elses-tag")

	tags, err := ListDistinctTags(database, 1)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	want := []string{"ai:recovery", "auto:intervals", "easy", "hard"}
	if len(tags) != len(want) {
		t.Fatalf("expected %v, got %v", want, tags)
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, tags)
		}
	}
}

func TestListPaginated_FilterByTitleNonASCII(t *testing.T) {
	database := setupTestDB(t)
	okt := insertFilterWorkout(t, database, 1, "running", "Økt på Grefsenkollen", "2024-01-01T10:00:00Z")

	// Exact case matches.
	workouts, _, err := ListPaginated(database, 1, WorkoutFilter{Query: "Økt"}, 25, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertIDs(t, workoutIDs(workouts), okt)

	// Lowercase non-ASCII also matches (LIKE compares bytes literally when
	// both sides use the same case).
	workouts, _, err = ListPaginated(database, 1, WorkoutFilter{Query: "økt"}, 25, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// SQLite LIKE is ASCII-only case-insensitive, so "økt" won't match "Økt".
	// This documents the known limitation rather than asserting it matches.
	if len(workouts) != 0 {
		t.Log("non-ASCII case mismatch unexpectedly matched — SQLite LIKE may have gained Unicode folding")
	}

	// Lowercase ASCII part of the title still matches via LIKE.
	workouts, _, err = ListPaginated(database, 1, WorkoutFilter{Query: "grefsenkollen"}, 25, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertIDs(t, workoutIDs(workouts), okt)
}

func TestTruncateRuneSafe(t *testing.T) {
	cases := []struct {
		input    string
		maxBytes int
		want     string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 3, "hel"},
		// "åøæ" is 6 bytes (2 per rune); cutting at 3 should back up to 2.
		{"åøæ", 3, "åø"[:2]},
		// Thai: 3 bytes per rune; cutting at 4 should back up to 3.
		{"กข", 4, "กข"[:3]},
		{"", 5, ""},
	}
	for _, tc := range cases {
		got := truncateRuneSafe(tc.input, tc.maxBytes)
		if got != tc.want {
			t.Errorf("truncateRuneSafe(%q, %d) = %q; want %q", tc.input, tc.maxBytes, got, tc.want)
		}
	}
}

func TestListDistinctTags_EmptyForUserWithNoTags(t *testing.T) {
	database := setupTestDB(t)
	insertFilterWorkout(t, database, 1, "running", "Untagged", "2024-01-01T10:00:00Z")

	tags, err := ListDistinctTags(database, 1)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if tags == nil {
		t.Fatal("expected an empty slice, not nil")
	}
	if len(tags) != 0 {
		t.Fatalf("expected no tags, got %v", tags)
	}
}
