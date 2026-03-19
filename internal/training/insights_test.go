package training

import (
	"encoding/json"
	"testing"
)

func TestBuildInsightsPrompt(t *testing.T) {
	w := &Workout{
		Sport:           "running",
		StartedAt:       "2026-02-21T10:00:00Z",
		DurationSeconds: 3065,
		DistanceMeters:  10000,
		AvgHeartRate:    155,
		MaxHeartRate:    178,
		AvgPaceSecPerKm: 306.5,
		AvgCadence:      182,
		Laps: []Lap{
			{LapNumber: 1, DurationSeconds: 360, DistanceMeters: 1000, AvgHeartRate: 150, MaxHeartRate: 160, AvgPaceSecPerKm: 360},
			{LapNumber: 2, DurationSeconds: 300, DistanceMeters: 1000, AvgHeartRate: 165, MaxHeartRate: 175, AvgPaceSecPerKm: 300},
		},
	}

	prompt := buildInsightsPrompt(w)

	if prompt == "" {
		t.Fatal("prompt should not be empty")
	}
	if !contains(prompt, "running") {
		t.Error("prompt should contain sport")
	}
	if !contains(prompt, "10.00 km") {
		t.Error("prompt should contain formatted distance")
	}
	if !contains(prompt, "155") {
		t.Error("prompt should contain avg HR")
	}
	if !contains(prompt, "| 1 |") {
		t.Error("prompt should contain lap table")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestParseInsightsResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name: "plain JSON",
			input: `{
				"effort_summary": "Solid aerobic run",
				"pacing_analysis": "Even pacing",
				"hr_zones": "Mostly zone 2-3",
				"observations": ["Good consistency"],
				"suggestions": ["Try negative splits"]
			}`,
		},
		{
			name: "markdown fenced",
			input: "```json\n" + `{
				"effort_summary": "Easy run",
				"pacing_analysis": "Steady",
				"hr_zones": "Zone 2",
				"observations": [],
				"suggestions": []
			}` + "\n```",
		},
		{
			name:    "invalid JSON",
			input:   "not json at all",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			insights, err := parseInsightsResponse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if insights.EffortSummary == "" {
				t.Error("effort_summary should not be empty")
			}
		})
	}
}

func TestCacheRoundTrip(t *testing.T) {
	db := setupTestDB(t)

	// Create a test user and workout.
	_, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'a@b.com', 'Test', 'gid1')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO workouts (id, user_id, sport, title, started_at, created_at) VALUES (1, 1, 'running', 'Test Run', '2026-02-21T10:00:00Z', '2026-02-21T10:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}

	// Initially no cache.
	cached, err := GetCachedInsights(db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached != nil {
		t.Fatal("expected nil for uncached workout")
	}

	// Save insights.
	insights := &TrainingInsights{
		EffortSummary:  "Good run",
		PacingAnalysis: "Even pacing",
		HRZones:        "Zone 2-3",
		Observations:   []string{"Nice and steady"},
		Suggestions:    []string{"Push harder next time"},
	}
	if err := SaveInsights(db, 1, insights, "claude-sonnet-4-6"); err != nil {
		t.Fatal(err)
	}

	// Retrieve cached.
	cached, err = GetCachedInsights(db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached == nil {
		t.Fatal("expected cached insights")
	}
	if cached.EffortSummary != "Good run" {
		t.Errorf("unexpected effort_summary: %s", cached.EffortSummary)
	}
	if cached.Model != "claude-sonnet-4-6" {
		t.Errorf("unexpected model: %s", cached.Model)
	}
	if !cached.Cached {
		t.Error("expected cached=true")
	}
	if len(cached.Observations) != 1 || cached.Observations[0] != "Nice and steady" {
		t.Errorf("unexpected observations: %v", cached.Observations)
	}

	// Verify JSON round-trip.
	data, _ := json.Marshal(cached)
	var roundTrip CachedInsights
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if roundTrip.EffortSummary != "Good run" {
		t.Errorf("round-trip failed: %s", roundTrip.EffortSummary)
	}
}
