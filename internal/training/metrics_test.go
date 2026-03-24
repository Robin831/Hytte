package training

import (
	"math"
	"testing"
	"time"
)

// --- ComputeHRDrift ---

func TestComputeHRDrift_NilWhenFewerThan10Samples(t *testing.T) {
	samples := make([]Sample, 9)
	for i := range samples {
		samples[i] = Sample{OffsetMs: int64(i * 10000), HeartRate: 140}
	}
	if got := ComputeHRDrift(samples, 3600); got != nil {
		t.Errorf("expected nil for <10 samples, got %v", *got)
	}
}

func TestComputeHRDrift_NilWhenZeroDuration(t *testing.T) {
	samples := make([]Sample, 20)
	for i := range samples {
		samples[i] = Sample{OffsetMs: int64(i * 1000), HeartRate: 140}
	}
	if got := ComputeHRDrift(samples, 0); got != nil {
		t.Errorf("expected nil for zero duration, got %v", *got)
	}
}

func TestComputeHRDrift_NilWhenAllSamplesInFirstHalf(t *testing.T) {
	// 20 samples all with offsets before the midpoint — secondCount will be 0
	samples := make([]Sample, 20)
	for i := range samples {
		samples[i] = Sample{OffsetMs: int64(i) * 100, HeartRate: 140}
	}
	// duration = 3600s, midpoint = 1800000ms; all samples are within first 1900ms
	if got := ComputeHRDrift(samples, 3600); got != nil {
		t.Errorf("expected nil when all samples in first half (secondCount=0), got %v", *got)
	}
}

func TestComputeHRDrift_NilWhenHRIsZero(t *testing.T) {
	// 20 samples, all with HeartRate=0
	samples := make([]Sample, 20)
	for i := range samples {
		samples[i] = Sample{OffsetMs: int64(i * 1000)}
	}
	if got := ComputeHRDrift(samples, 3600); got != nil {
		t.Errorf("expected nil when all HR=0, got %v", *got)
	}
}

func TestComputeHRDrift_PositiveDrift(t *testing.T) {
	// 20 samples: first 10 at 140 bpm, last 10 at 154 bpm
	// duration = 20s, midpoint = 10s
	samples := make([]Sample, 20)
	for i := 0; i < 10; i++ {
		samples[i] = Sample{OffsetMs: int64(i) * 1000, HeartRate: 140}
	}
	for i := 10; i < 20; i++ {
		samples[i] = Sample{OffsetMs: int64(i) * 1000, HeartRate: 154}
	}
	// duration = 20s, midpointMs = 10000ms
	got := ComputeHRDrift(samples, 20)
	if got == nil {
		t.Fatal("expected non-nil drift")
	}
	// (154-140)/140*100 = 10.0
	want := 10.0
	if math.Abs(*got-want) > 0.001 {
		t.Errorf("drift: want %.4f, got %.4f", want, *got)
	}
}

func TestComputeHRDrift_NegativeDrift(t *testing.T) {
	// 20 samples: first 10 at 160 bpm, last 10 at 140 bpm
	samples := make([]Sample, 20)
	for i := 0; i < 10; i++ {
		samples[i] = Sample{OffsetMs: int64(i) * 1000, HeartRate: 160}
	}
	for i := 10; i < 20; i++ {
		samples[i] = Sample{OffsetMs: int64(i) * 1000, HeartRate: 140}
	}
	got := ComputeHRDrift(samples, 20)
	if got == nil {
		t.Fatal("expected non-nil drift")
	}
	// (140-160)/160*100 = -12.5
	want := -12.5
	if math.Abs(*got-want) > 0.001 {
		t.Errorf("drift: want %.4f, got %.4f", want, *got)
	}
}

func TestComputeHRDrift_ZeroDrift(t *testing.T) {
	// 20 samples all at same HR
	samples := make([]Sample, 20)
	for i := range samples {
		samples[i] = Sample{OffsetMs: int64(i) * 1000, HeartRate: 150}
	}
	got := ComputeHRDrift(samples, 20)
	if got == nil {
		t.Fatal("expected non-nil drift")
	}
	if math.Abs(*got) > 0.001 {
		t.Errorf("expected zero drift, got %.4f", *got)
	}
}

// --- ComputePaceCV ---

func TestComputePaceCV_NilWhenFewerThan10Samples(t *testing.T) {
	samples := make([]Sample, 9)
	for i := range samples {
		samples[i] = Sample{SpeedMPerS: 3.0}
	}
	if got := ComputePaceCV(samples); got != nil {
		t.Errorf("expected nil for <10 samples, got %v", *got)
	}
}

func TestComputePaceCV_NilWhenSpeedIsZero(t *testing.T) {
	samples := make([]Sample, 20)
	// all zero speed
	if got := ComputePaceCV(samples); got != nil {
		t.Errorf("expected nil when speed=0, got %v", *got)
	}
}

func TestComputePaceCV_ConstantSpeed(t *testing.T) {
	// Constant speed → CV should be ~0
	samples := make([]Sample, 15)
	for i := range samples {
		samples[i] = Sample{SpeedMPerS: 3.5}
	}
	got := ComputePaceCV(samples)
	if got == nil {
		t.Fatal("expected non-nil CV")
	}
	if math.Abs(*got) > 0.001 {
		t.Errorf("expected near-zero CV for constant speed, got %.4f", *got)
	}
}

func TestComputePaceCV_VaryingSpeed(t *testing.T) {
	// 10 samples alternating between two speeds: 2.5 and 5.0 m/s
	// paces: 400 and 200 sec/km; mean=300, variance=((100^2+100^2)/2)=10000, stddev=100
	// CV = 100/300*100 = 33.333...
	samples := make([]Sample, 10)
	for i := range samples {
		if i%2 == 0 {
			samples[i] = Sample{SpeedMPerS: 2.5}
		} else {
			samples[i] = Sample{SpeedMPerS: 5.0}
		}
	}
	got := ComputePaceCV(samples)
	if got == nil {
		t.Fatal("expected non-nil CV")
	}
	want := 100.0 / 300.0 * 100.0
	if math.Abs(*got-want) > 0.01 {
		t.Errorf("CV: want %.4f, got %.4f", want, *got)
	}
}

// --- ComputeTrainingLoad ---

func TestComputeTrainingLoad_NilWhenAvgHRZero(t *testing.T) {
	if got := ComputeTrainingLoad(60, 0, 180); got != nil {
		t.Errorf("expected nil when avgHR=0, got %v", *got)
	}
}

func TestComputeTrainingLoad_NilWhenMaxHRZero(t *testing.T) {
	if got := ComputeTrainingLoad(60, 150, 0); got != nil {
		t.Errorf("expected nil when maxHR=0, got %v", *got)
	}
}

func TestComputeTrainingLoad_BasicCalculation(t *testing.T) {
	// 60 minutes * 150/180 = 60 * 0.8333... = 50.0
	got := ComputeTrainingLoad(60, 150, 180)
	if got == nil {
		t.Fatal("expected non-nil training load")
	}
	want := 60.0 * 150.0 / 180.0
	if math.Abs(*got-want) > 0.001 {
		t.Errorf("load: want %.4f, got %.4f", want, *got)
	}
}

func TestComputeTrainingLoad_MaxEffort(t *testing.T) {
	// avgHR == maxHR → load == duration
	got := ComputeTrainingLoad(45, 180, 180)
	if got == nil {
		t.Fatal("expected non-nil training load")
	}
	if math.Abs(*got-45.0) > 0.001 {
		t.Errorf("load: want 45.0, got %.4f", *got)
	}
}

// --- ComputeACR ---

func TestComputeACR_NilWhenNoData(t *testing.T) {
	db := setupTestDB(t)
	ratio, acute, chronic, err := ComputeACR(db, 1, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ratio != nil {
		t.Errorf("expected nil ratio with no data, got %v", *ratio)
	}
	if acute != 0 || chronic != 0 {
		t.Errorf("expected zero acute/chronic, got acute=%v chronic=%v", acute, chronic)
	}
}

func TestComputeACR_NilRatioWhenChronicZero(t *testing.T) {
	// Insert a workout for user 1 so the DB is not empty, then query for a
	// different user. This verifies user scoping: user 999 has no load, so
	// chronic==0 and the ratio must be nil.
	db := setupTestDB(t)
	asOf := time.Now().UTC()

	ts := asOf.AddDate(0, 0, -3).Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters, fit_file_hash, training_load)
		 VALUES (?, ?, ?, ?, ?, 0, ?, ?)`,
		1, "running", "User1 Workout", ts, 3600, "acrcz1", 50.0,
	)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	ratio, acute, chronic, err := ComputeACR(db, 999, asOf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ratio != nil {
		t.Errorf("expected nil ratio for unknown user, got %v", *ratio)
	}
	if acute != 0 || chronic != 0 {
		t.Errorf("expected zero acute/chronic for unknown user, got acute=%v chronic=%v", acute, chronic)
	}
}

func TestComputeACR_BasicRatio(t *testing.T) {
	db := setupTestDB(t)
	asOf := time.Now().UTC()

	// Insert 4 workouts: one per week in the past 28 days, each load=40.
	// 28-day chronic = (40*4)/4 = 40
	// The most recent workout is 2 days ago → inside 7-day window → acute = 40
	// ACR = 40/40 = 1.0
	days := []int{2, 9, 16, 23}
	for i, d := range days {
		ts := asOf.AddDate(0, 0, -d).Format(time.RFC3339)
		hash := "acrhash" + string(rune('a'+i))
		tl := 40.0
		_, err := db.Exec(
			`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters, fit_file_hash, training_load)
			 VALUES (?, ?, ?, ?, ?, 0, ?, ?)`,
			1, "running", "ACR Test", ts, 3600, hash, tl,
		)
		if err != nil {
			t.Fatalf("insert workout: %v", err)
		}
	}

	ratio, acute, chronic, err := ComputeACR(db, 1, asOf)
	if err != nil {
		t.Fatalf("ComputeACR error: %v", err)
	}
	if ratio == nil {
		t.Fatal("expected non-nil ACR ratio")
	}
	if math.Abs(acute-40.0) > 0.01 {
		t.Errorf("acute: want 40.0, got %.4f", acute)
	}
	if math.Abs(chronic-40.0) > 0.01 {
		t.Errorf("chronic: want 40.0, got %.4f", chronic)
	}
	if math.Abs(*ratio-1.0) > 0.01 {
		t.Errorf("ACR ratio: want 1.0, got %.4f", *ratio)
	}
}

func TestComputeACR_HighAcuteLoad(t *testing.T) {
	db := setupTestDB(t)
	asOf := time.Now().UTC()

	// 1 workout 2 days ago with load=100 (acute), 3 old workouts at load=25 each.
	// chronic = (100 + 25*3) / 4 = 175/4 = 43.75
	// acute = 100
	// ACR = 100/43.75 ≈ 2.2857
	entries := []struct {
		daysAgo int
		load    float64
		hash    string
	}{
		{2, 100, "acrhi1"},
		{10, 25, "acrhi2"},
		{17, 25, "acrhi3"},
		{24, 25, "acrhi4"},
	}
	for _, e := range entries {
		ts := asOf.AddDate(0, 0, -e.daysAgo).Format(time.RFC3339)
		_, err := db.Exec(
			`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters, fit_file_hash, training_load)
			 VALUES (?, ?, ?, ?, ?, 0, ?, ?)`,
			1, "running", "ACR High", ts, 3600, e.hash, e.load,
		)
		if err != nil {
			t.Fatalf("insert workout: %v", err)
		}
	}

	ratio, acute, chronic, err := ComputeACR(db, 1, asOf)
	if err != nil {
		t.Fatalf("ComputeACR error: %v", err)
	}
	if ratio == nil {
		t.Fatal("expected non-nil ACR ratio")
	}
	wantAcute := 100.0
	wantChronic := 175.0 / 4.0
	wantRatio := wantAcute / wantChronic
	if math.Abs(acute-wantAcute) > 0.01 {
		t.Errorf("acute: want %.4f, got %.4f", wantAcute, acute)
	}
	if math.Abs(chronic-wantChronic) > 0.01 {
		t.Errorf("chronic: want %.4f, got %.4f", wantChronic, chronic)
	}
	if math.Abs(*ratio-wantRatio) > 0.01 {
		t.Errorf("ACR ratio: want %.4f, got %.4f", wantRatio, *ratio)
	}
}

func TestComputeACR_IgnoresNullTrainingLoad(t *testing.T) {
	db := setupTestDB(t)
	asOf := time.Now().UTC()

	// Insert a workout with NULL training_load — should be ignored.
	ts := asOf.AddDate(0, 0, -3).Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters, fit_file_hash)
		 VALUES (?, ?, ?, ?, ?, 0, ?)`,
		1, "running", "No Load", ts, 3600, "acrnull1",
	)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	ratio, acute, chronic, err := ComputeACR(db, 1, asOf)
	if err != nil {
		t.Fatalf("ComputeACR error: %v", err)
	}
	if ratio != nil {
		t.Errorf("expected nil ratio when only null loads present, got %v", *ratio)
	}
	if acute != 0 || chronic != 0 {
		t.Errorf("expected zero values, got acute=%v chronic=%v", acute, chronic)
	}
}

func TestComputeACR_ExcludesWorkoutsOlderThan28Days(t *testing.T) {
	db := setupTestDB(t)
	asOf := time.Now().UTC()

	// Insert a workout exactly 29 days ago — should be excluded.
	ts := asOf.AddDate(0, 0, -29).Format(time.RFC3339)
	tl := 80.0
	_, err := db.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters, fit_file_hash, training_load)
		 VALUES (?, ?, ?, ?, ?, 0, ?, ?)`,
		1, "running", "Old", ts, 3600, "acrold1", tl,
	)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	ratio, acute, chronic, err := ComputeACR(db, 1, asOf)
	if err != nil {
		t.Fatalf("ComputeACR error: %v", err)
	}
	if ratio != nil {
		t.Errorf("expected nil ratio (old workout excluded), got %v", *ratio)
	}
	if acute != 0 || chronic != 0 {
		t.Errorf("expected zero values for excluded workout, got acute=%v chronic=%v", acute, chronic)
	}
}
