package training

import (
	"database/sql"
	"testing"
	"time"
)

func TestUpsertAndGetWeeklyLoads(t *testing.T) {
	db := setupTestDB(t)

	now := time.Date(2026, 3, 17, 0, 0, 0, 0, time.UTC)
	wl := WeeklyLoad{
		UserID:       1,
		WeekStart:    now.Format("2006-01-02"),
		EasyLoad:     100.0,
		HardLoad:     50.0,
		TotalLoad:    150.0,
		WorkoutCount: 3,
		UpdatedAt:    now.Format(time.RFC3339),
	}

	if err := UpsertWeeklyLoad(db, wl); err != nil {
		t.Fatalf("UpsertWeeklyLoad: %v", err)
	}

	loads, err := GetWeeklyLoads(db, 1, 10)
	if err != nil {
		t.Fatalf("GetWeeklyLoads: %v", err)
	}
	if len(loads) != 1 {
		t.Fatalf("expected 1 load, got %d", len(loads))
	}
	got := loads[0]
	if got.EasyLoad != 100.0 {
		t.Errorf("EasyLoad: want 100.0, got %v", got.EasyLoad)
	}
	if got.HardLoad != 50.0 {
		t.Errorf("HardLoad: want 50.0, got %v", got.HardLoad)
	}
	if got.TotalLoad != 150.0 {
		t.Errorf("TotalLoad: want 150.0, got %v", got.TotalLoad)
	}
	if got.WorkoutCount != 3 {
		t.Errorf("WorkoutCount: want 3, got %v", got.WorkoutCount)
	}
}

func TestUpsertWeeklyLoad_UpdatesExistingRow(t *testing.T) {
	db := setupTestDB(t)

	ws := "2026-03-17"
	first := WeeklyLoad{
		UserID: 1, WeekStart: ws, TotalLoad: 100.0, WorkoutCount: 2,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := UpsertWeeklyLoad(db, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second := WeeklyLoad{
		UserID: 1, WeekStart: ws, EasyLoad: 60.0, HardLoad: 80.0, TotalLoad: 140.0,
		WorkoutCount: 3, UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := UpsertWeeklyLoad(db, second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	loads, err := GetWeeklyLoads(db, 1, 10)
	if err != nil {
		t.Fatalf("GetWeeklyLoads: %v", err)
	}
	if len(loads) != 1 {
		t.Fatalf("expected 1 load after upsert, got %d", len(loads))
	}
	if loads[0].TotalLoad != 140.0 {
		t.Errorf("TotalLoad after update: want 140.0, got %v", loads[0].TotalLoad)
	}
	if loads[0].WorkoutCount != 3 {
		t.Errorf("WorkoutCount after update: want 3, got %v", loads[0].WorkoutCount)
	}
}

func TestGetWeeklyLoads_OrderedDescending(t *testing.T) {
	db := setupTestDB(t)

	weeks := []string{"2026-03-03", "2026-03-10", "2026-03-17"}
	for _, ws := range weeks {
		wl := WeeklyLoad{
			UserID: 1, WeekStart: ws, TotalLoad: 50.0,
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		}
		if err := UpsertWeeklyLoad(db, wl); err != nil {
			t.Fatalf("UpsertWeeklyLoad(%s): %v", ws, err)
		}
	}

	loads, err := GetWeeklyLoads(db, 1, 10)
	if err != nil {
		t.Fatalf("GetWeeklyLoads: %v", err)
	}
	if len(loads) != 3 {
		t.Fatalf("expected 3 loads, got %d", len(loads))
	}
	if loads[0].WeekStart != "2026-03-17" {
		t.Errorf("first load week_start: want 2026-03-17, got %s", loads[0].WeekStart)
	}
	if loads[2].WeekStart != "2026-03-03" {
		t.Errorf("last load week_start: want 2026-03-03, got %s", loads[2].WeekStart)
	}
}

func TestGetWeeklyLoads_LimitRespected(t *testing.T) {
	db := setupTestDB(t)

	for i := 0; i < 5; i++ {
		ws := time.Date(2026, 3, 17-i*7, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		wl := WeeklyLoad{
			UserID: 1, WeekStart: ws, TotalLoad: 50.0,
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		}
		if err := UpsertWeeklyLoad(db, wl); err != nil {
			t.Fatalf("UpsertWeeklyLoad: %v", err)
		}
	}

	loads, err := GetWeeklyLoads(db, 1, 3)
	if err != nil {
		t.Fatalf("GetWeeklyLoads: %v", err)
	}
	if len(loads) != 3 {
		t.Errorf("expected 3 loads (limit), got %d", len(loads))
	}
}

func TestGetWeeklyLoads_IsolatedByUser(t *testing.T) {
	db := setupTestDB(t)

	if _, err := db.Exec(
		`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'google-2')`,
	); err != nil {
		t.Fatalf("insert user 2: %v", err)
	}

	wl1 := WeeklyLoad{UserID: 1, WeekStart: "2026-03-17", TotalLoad: 100.0, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	wl2 := WeeklyLoad{UserID: 2, WeekStart: "2026-03-17", TotalLoad: 200.0, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := UpsertWeeklyLoad(db, wl1); err != nil {
		t.Fatalf("UpsertWeeklyLoad user 1: %v", err)
	}
	if err := UpsertWeeklyLoad(db, wl2); err != nil {
		t.Fatalf("UpsertWeeklyLoad user 2: %v", err)
	}

	loads, err := GetWeeklyLoads(db, 1, 10)
	if err != nil {
		t.Fatalf("GetWeeklyLoads: %v", err)
	}
	if len(loads) != 1 || loads[0].UserID != 1 {
		t.Errorf("expected 1 load for user 1, got %d", len(loads))
	}
}

func TestRefreshWeeklyLoad_NoWorkouts(t *testing.T) {
	db := setupTestDB(t)
	ws := time.Date(2026, 3, 17, 0, 0, 0, 0, time.UTC)

	wl, err := RefreshWeeklyLoad(db, 1, ws)
	if err != nil {
		t.Fatalf("RefreshWeeklyLoad: %v", err)
	}
	if wl.TotalLoad != 0 {
		t.Errorf("TotalLoad: want 0, got %v", wl.TotalLoad)
	}
	if wl.WorkoutCount != 0 {
		t.Errorf("WorkoutCount: want 0, got %v", wl.WorkoutCount)
	}
	if wl.WeekStart != "2026-03-17" {
		t.Errorf("WeekStart: want 2026-03-17, got %s", wl.WeekStart)
	}
}

func TestRefreshWeeklyLoad_SplitsEasyHard(t *testing.T) {
	db := setupTestDB(t)
	ws := time.Date(2026, 3, 17, 0, 0, 0, 0, time.UTC)

	// Default max HR = 190, threshold = 152.
	// Easy workout: avg HR 130 (< 152), load = 60.
	// Hard workout: avg HR 165 (>= 152), load = 90.
	easyStartedAt := ws.Add(time.Hour).Format(time.RFC3339)
	hardStartedAt := ws.Add(25 * time.Hour).Format(time.RFC3339)

	if _, err := db.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, fit_file_hash, avg_heart_rate, training_load)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		1, "running", "Easy Run", easyStartedAt, 3600, "easyhash", 130, 60.0,
	); err != nil {
		t.Fatalf("insert easy workout: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, fit_file_hash, avg_heart_rate, training_load)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		1, "running", "Hard Run", hardStartedAt, 3600, "hardhash", 165, 90.0,
	); err != nil {
		t.Fatalf("insert hard workout: %v", err)
	}

	wl, err := RefreshWeeklyLoad(db, 1, ws)
	if err != nil {
		t.Fatalf("RefreshWeeklyLoad: %v", err)
	}
	if wl.EasyLoad != 60.0 {
		t.Errorf("EasyLoad: want 60.0, got %v", wl.EasyLoad)
	}
	if wl.HardLoad != 90.0 {
		t.Errorf("HardLoad: want 90.0, got %v", wl.HardLoad)
	}
	if wl.TotalLoad != 150.0 {
		t.Errorf("TotalLoad: want 150.0, got %v", wl.TotalLoad)
	}
	if wl.WorkoutCount != 2 {
		t.Errorf("WorkoutCount: want 2, got %v", wl.WorkoutCount)
	}
}

func TestRefreshWeeklyLoad_UsesMaxHRPreference(t *testing.T) {
	db := setupTestDB(t)
	ws := time.Date(2026, 3, 17, 0, 0, 0, 0, time.UTC)

	// Set max HR to 200; threshold = 160.
	if _, err := db.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'max_hr', '200')`,
	); err != nil {
		t.Fatalf("set max_hr preference: %v", err)
	}

	// avg HR 162 (>= 160) → should be hard.
	startedAt := ws.Add(time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, fit_file_hash, avg_heart_rate, training_load)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		1, "running", "Borderline Run", startedAt, 3600, "borderinghash", 162, 80.0,
	); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	wl, err := RefreshWeeklyLoad(db, 1, ws)
	if err != nil {
		t.Fatalf("RefreshWeeklyLoad: %v", err)
	}
	if wl.HardLoad != 80.0 {
		t.Errorf("HardLoad: want 80.0 (workout classified as hard), got %v", wl.HardLoad)
	}
	if wl.EasyLoad != 0 {
		t.Errorf("EasyLoad: want 0, got %v", wl.EasyLoad)
	}
}

func TestRefreshWeeklyLoad_NullAvgHR_CountedAsEasy(t *testing.T) {
	db := setupTestDB(t)
	ws := time.Date(2026, 3, 17, 0, 0, 0, 0, time.UTC)

	// GPS-only session: has training_load but avg_heart_rate is NULL.
	startedAt := ws.Add(time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, fit_file_hash, training_load)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		1, "cycling", "GPS Only Ride", startedAt, 3600, "gpsonlyhash", 75.0,
	); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	wl, err := RefreshWeeklyLoad(db, 1, ws)
	if err != nil {
		t.Fatalf("RefreshWeeklyLoad crashed on NULL avg_heart_rate: %v", err)
	}
	if wl.WorkoutCount != 1 {
		t.Errorf("WorkoutCount: want 1, got %v", wl.WorkoutCount)
	}
	if wl.EasyLoad != 75.0 {
		t.Errorf("EasyLoad: want 75.0 (NULL HR treated as easy), got %v", wl.EasyLoad)
	}
	if wl.HardLoad != 0 {
		t.Errorf("HardLoad: want 0, got %v", wl.HardLoad)
	}
	if wl.TotalLoad != 75.0 {
		t.Errorf("TotalLoad: want 75.0, got %v", wl.TotalLoad)
	}
}

func TestRefreshWeeklyLoad_ExcludesWorkoutsWithoutLoad(t *testing.T) {
	db := setupTestDB(t)
	ws := time.Date(2026, 3, 17, 0, 0, 0, 0, time.UTC)

	startedAt := ws.Add(time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, fit_file_hash)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		1, "running", "No Load Run", startedAt, 3600, "noloadhash",
	); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	wl, err := RefreshWeeklyLoad(db, 1, ws)
	if err != nil {
		t.Fatalf("RefreshWeeklyLoad: %v", err)
	}
	if wl.WorkoutCount != 0 {
		t.Errorf("WorkoutCount: want 0 (workout without load excluded), got %v", wl.WorkoutCount)
	}
}

func TestRefreshWeeklyLoad_ExcludesWorkoutsOutsideWindow(t *testing.T) {
	db := setupTestDB(t)
	ws := time.Date(2026, 3, 17, 0, 0, 0, 0, time.UTC)

	// Insert a workout one week before the window and one one day after.
	beforeWS := ws.AddDate(0, 0, -1).Format(time.RFC3339)
	afterWE := ws.AddDate(0, 0, 7).Format(time.RFC3339)

	for i, startedAt := range []string{beforeWS, afterWE} {
		hash := "outofwindow" + string(rune('0'+i))
		if _, err := db.Exec(
			`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, fit_file_hash, training_load)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			1, "running", "Out-of-window", startedAt, 3600, hash, 50.0,
		); err != nil {
			t.Fatalf("insert workout %d: %v", i, err)
		}
	}

	wl, err := RefreshWeeklyLoad(db, 1, ws)
	if err != nil {
		t.Fatalf("RefreshWeeklyLoad: %v", err)
	}
	if wl.WorkoutCount != 0 {
		t.Errorf("WorkoutCount: want 0 (out-of-window workouts excluded), got %v", wl.WorkoutCount)
	}
}

func TestRefreshWeeklyLoad_PersistedAfterRefresh(t *testing.T) {
	db := setupTestDB(t)
	ws := time.Date(2026, 3, 17, 0, 0, 0, 0, time.UTC)

	startedAt := ws.Add(time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, fit_file_hash, avg_heart_rate, training_load)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		1, "running", "Run", startedAt, 3600, "persisthash", 140, 70.0,
	); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	if _, err := RefreshWeeklyLoad(db, 1, ws); err != nil {
		t.Fatalf("RefreshWeeklyLoad: %v", err)
	}

	loads, err := GetWeeklyLoads(db, 1, 10)
	if err != nil {
		t.Fatalf("GetWeeklyLoads after refresh: %v", err)
	}
	if len(loads) != 1 {
		t.Fatalf("expected 1 persisted load, got %d", len(loads))
	}
	if loads[0].TotalLoad != 70.0 {
		t.Errorf("persisted TotalLoad: want 70.0, got %v", loads[0].TotalLoad)
	}
}

func TestUpsertTrainingSummary_RoundTrip(t *testing.T) {
	db := setupTestDB(t)

	acr := 1.1
	s := TrainingSummary{
		UserID:      1,
		WeekStart:   "2026-03-17",
		Status:      string(StatusOptimal),
		ACR:         &acr,
		AcuteLoad:   110.0,
		ChronicLoad: 100.0,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	if err := UpsertTrainingSummary(db, s); err != nil {
		t.Fatalf("UpsertTrainingSummary: %v", err)
	}

	got, err := GetLatestTrainingSummary(db, 1)
	if err != nil {
		t.Fatalf("GetLatestTrainingSummary: %v", err)
	}
	if got.Status != string(StatusOptimal) {
		t.Errorf("Status: want %s, got %s", StatusOptimal, got.Status)
	}
	if got.ACR == nil || *got.ACR != acr {
		t.Errorf("ACR: want %v, got %v", acr, got.ACR)
	}
	if got.AcuteLoad != 110.0 {
		t.Errorf("AcuteLoad: want 110.0, got %v", got.AcuteLoad)
	}
}

func TestUpsertTrainingSummary_UpdatesExistingRow(t *testing.T) {
	db := setupTestDB(t)

	ws := "2026-03-17"
	first := TrainingSummary{
		UserID: 1, WeekStart: ws, Status: string(StatusIncreasing),
		AcuteLoad: 120.0, ChronicLoad: 100.0,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := UpsertTrainingSummary(db, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second := TrainingSummary{
		UserID: 1, WeekStart: ws, Status: string(StatusOptimal),
		AcuteLoad: 100.0, ChronicLoad: 100.0,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := UpsertTrainingSummary(db, second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := GetLatestTrainingSummary(db, 1)
	if err != nil {
		t.Fatalf("GetLatestTrainingSummary: %v", err)
	}
	if got.Status != string(StatusOptimal) {
		t.Errorf("Status after update: want %s, got %s", StatusOptimal, got.Status)
	}
}

func TestGetLatestTrainingSummary_NoRows(t *testing.T) {
	db := setupTestDB(t)

	_, err := GetLatestTrainingSummary(db, 1)
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestGetLatestTrainingSummary_ReturnsNewest(t *testing.T) {
	db := setupTestDB(t)

	for _, ws := range []string{"2026-03-03", "2026-03-10", "2026-03-17"} {
		s := TrainingSummary{
			UserID: 1, WeekStart: ws, Status: string(StatusOptimal),
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		}
		if err := UpsertTrainingSummary(db, s); err != nil {
			t.Fatalf("UpsertTrainingSummary(%s): %v", ws, err)
		}
	}

	got, err := GetLatestTrainingSummary(db, 1)
	if err != nil {
		t.Fatalf("GetLatestTrainingSummary: %v", err)
	}
	if got.WeekStart != "2026-03-17" {
		t.Errorf("WeekStart: want 2026-03-17, got %s", got.WeekStart)
	}
}
