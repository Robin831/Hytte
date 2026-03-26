package daemon

import (
	"testing"
	"time"
)

func TestShouldFireStreakWarning(t *testing.T) {
	utc := time.UTC
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Skip("Europe/Oslo timezone not available")
	}

	// A fixed Monday in UTC at 19:30.
	baseUTC := time.Date(2025, 3, 10, 19, 30, 0, 0, time.UTC)

	tests := []struct {
		name      string
		loc       *time.Location
		lastSent  string
		now       time.Time
		wantFire  bool
		wantKey   string
	}{
		{
			name:     "UTC 7PM fires",
			loc:      utc,
			lastSent: "",
			now:      baseUTC,
			wantFire: true,
			wantKey:  "2025-03-10",
		},
		{
			name:     "UTC 7PM already sent today",
			loc:      utc,
			lastSent: "2025-03-10",
			now:      baseUTC,
			wantFire: false,
		},
		{
			name:     "UTC 8PM does not fire",
			loc:      utc,
			lastSent: "",
			now:      time.Date(2025, 3, 10, 20, 0, 0, 0, time.UTC),
			wantFire: false,
		},
		{
			name:     "UTC 6PM does not fire",
			loc:      utc,
			lastSent: "",
			now:      time.Date(2025, 3, 10, 18, 59, 0, 0, time.UTC),
			wantFire: false,
		},
		{
			name:     "Oslo timezone: UTC 18:00 is Oslo 19:00 (CET)",
			loc:      oslo,
			lastSent: "",
			// CET = UTC+1, so UTC 18:00 = Oslo 19:00
			now:      time.Date(2025, 3, 10, 18, 0, 0, 0, time.UTC),
			wantFire: true,
			wantKey:  "2025-03-10",
		},
		{
			name:     "Oslo timezone: fires for new day after previous sent",
			loc:      oslo,
			lastSent: "2025-03-09",
			now:      time.Date(2025, 3, 10, 18, 0, 0, 0, time.UTC),
			wantFire: true,
			wantKey:  "2025-03-10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFire, gotKey := shouldFireStreakWarning(tt.loc, tt.lastSent, tt.now)
			if gotFire != tt.wantFire {
				t.Errorf("shouldFireStreakWarning() fire = %v, want %v", gotFire, tt.wantFire)
			}
			if tt.wantFire && gotKey != tt.wantKey {
				t.Errorf("shouldFireStreakWarning() key = %q, want %q", gotKey, tt.wantKey)
			}
		})
	}
}

func TestShouldFireWeeklySummary(t *testing.T) {
	utc := time.UTC
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Skip("Europe/Oslo timezone not available")
	}

	// 2025-03-10 is a Monday.
	mondayUTC8 := time.Date(2025, 3, 10, 8, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		loc       *time.Location
		lastSent  string
		now       time.Time
		wantFire  bool
		wantKey   string
	}{
		{
			name:     "Monday 8AM UTC fires",
			loc:      utc,
			lastSent: "",
			now:      mondayUTC8,
			wantFire: true,
			wantKey:  "2025-W11",
		},
		{
			name:     "Monday 8AM UTC already sent this week",
			loc:      utc,
			lastSent: "2025-W11",
			now:      mondayUTC8,
			wantFire: false,
		},
		{
			name:     "Monday 9AM UTC does not fire",
			loc:      utc,
			lastSent: "",
			now:      time.Date(2025, 3, 10, 9, 0, 0, 0, time.UTC),
			wantFire: false,
		},
		{
			name:     "Tuesday 8AM UTC does not fire",
			loc:      utc,
			lastSent: "",
			now:      time.Date(2025, 3, 11, 8, 0, 0, 0, time.UTC),
			wantFire: false,
		},
		{
			name:     "Sunday does not fire",
			loc:      utc,
			lastSent: "",
			now:      time.Date(2025, 3, 9, 8, 0, 0, 0, time.UTC),
			wantFire: false,
		},
		{
			name:     "Oslo: UTC 7AM on Monday = Oslo 8AM (CET)",
			loc:      oslo,
			lastSent: "",
			// CET = UTC+1, so UTC 07:00 = Oslo 08:00
			now:      time.Date(2025, 3, 10, 7, 0, 0, 0, time.UTC),
			wantFire: true,
			wantKey:  "2025-W11",
		},
		{
			name:     "Fires again for a new week",
			loc:      utc,
			lastSent: "2025-W10",
			now:      mondayUTC8,
			wantFire: true,
			wantKey:  "2025-W11",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFire, gotKey := shouldFireWeeklySummary(tt.loc, tt.lastSent, tt.now)
			if gotFire != tt.wantFire {
				t.Errorf("shouldFireWeeklySummary() fire = %v, want %v", gotFire, tt.wantFire)
			}
			if tt.wantFire && gotKey != tt.wantKey {
				t.Errorf("shouldFireWeeklySummary() key = %q, want %q", gotKey, tt.wantKey)
			}
		})
	}
}

func TestUserLocation(t *testing.T) {
	tests := []struct {
		name     string
		prefs    map[string]string
		wantName string
	}{
		{
			name:     "no timezone preference falls back to UTC",
			prefs:    map[string]string{},
			wantName: "UTC",
		},
		{
			name:     "valid timezone is used",
			prefs:    map[string]string{"quiet_hours_timezone": "Europe/Oslo"},
			wantName: "Europe/Oslo",
		},
		{
			name:     "invalid timezone falls back to UTC",
			prefs:    map[string]string{"quiet_hours_timezone": "Not/A/Timezone"},
			wantName: "UTC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc := userLocation(tt.prefs)
			if loc.String() != tt.wantName {
				t.Errorf("userLocation() = %q, want %q", loc.String(), tt.wantName)
			}
		})
	}
}

func TestShouldFireWeeklyChallenges(t *testing.T) {
	// 2025-03-10 is a Monday.
	tests := []struct {
		name     string
		now      time.Time
		wantFire bool
	}{
		{
			name:     "Monday at exactly 08:00 fires",
			now:      time.Date(2025, 3, 10, 8, 0, 0, 0, time.UTC),
			wantFire: true,
		},
		{
			name:     "Monday at 08:30 fires",
			now:      time.Date(2025, 3, 10, 8, 30, 0, 0, time.UTC),
			wantFire: true,
		},
		{
			name:     "Monday at 15:00 fires (daemon was down at 08:xx)",
			now:      time.Date(2025, 3, 10, 15, 0, 0, 0, time.UTC),
			wantFire: true,
		},
		{
			name:     "Monday at 07:59 does not fire",
			now:      time.Date(2025, 3, 10, 7, 59, 0, 0, time.UTC),
			wantFire: false,
		},
		{
			name:     "Tuesday at 08:00 does not fire",
			now:      time.Date(2025, 3, 11, 8, 0, 0, 0, time.UTC),
			wantFire: false,
		},
		{
			name:     "Sunday at 08:00 does not fire",
			now:      time.Date(2025, 3, 9, 8, 0, 0, 0, time.UTC),
			wantFire: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldFireWeeklyChallenges(tt.now)
			if got != tt.wantFire {
				t.Errorf("shouldFireWeeklyChallenges() = %v, want %v", got, tt.wantFire)
			}
		})
	}
}

func TestISOWeekMonday(t *testing.T) {
	// 2025-W11 should start on Monday 2025-03-10.
	got := isoWeekMonday(2025, 11)
	want := time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("isoWeekMonday(2025, 11) = %v, want %v", got, want)
	}
}
