package workhours

import (
	"testing"
)

func TestCalculateDay_BasicSession(t *testing.T) {
	settings := DefaultSettings()
	day := WorkDay{
		Date:  "2026-03-27",
		Lunch: true,
		Sessions: []WorkSession{
			{StartTime: "06:00", EndTime: "15:00"}, // 9h = 540 min
		},
	}

	sum, err := CalculateDay(day, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sum.GrossMinutes != 540 {
		t.Errorf("gross: got %d, want 540", sum.GrossMinutes)
	}
	if sum.LunchMinutes != 30 {
		t.Errorf("lunch: got %d, want 30", sum.LunchMinutes)
	}
	if sum.NetMinutes != 510 {
		t.Errorf("net: got %d, want 510", sum.NetMinutes)
	}
	// 510 / 30 = 17 intervals → 510 reported (rounds to same)
	if sum.ReportedMinutes != 510 {
		t.Errorf("reported: got %d, want 510", sum.ReportedMinutes)
	}
	if sum.ReportedHours != 8.5 {
		t.Errorf("reported hours: got %g, want 8.5", sum.ReportedHours)
	}
	if sum.RemainderMinutes != 0 {
		t.Errorf("remainder: got %d, want 0", sum.RemainderMinutes)
	}
}

func TestCalculateDay_Remainder(t *testing.T) {
	// Plan example: 457 net minutes → 7.5h reported, +7 min remainder
	settings := DefaultSettings()
	day := WorkDay{
		Date:  "2026-03-27",
		Lunch: true,
		Sessions: []WorkSession{
			{StartTime: "06:00", EndTime: "15:07"}, // 9h 7min = 547 min gross
		},
		// 547 - 30 lunch = 517... hmm, let me construct 457 net exactly
		// 457 net = 457 + 30 lunch = 487 gross, so session is 487 min = 8h7min
	}

	// Reconfigure: start 06:00, end 14:07 = 487 min
	day.Sessions = []WorkSession{
		{StartTime: "06:00", EndTime: "14:07"}, // 8h7min = 487 min
	}

	sum, err := CalculateDay(day, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sum.GrossMinutes != 487 {
		t.Errorf("gross: got %d, want 487", sum.GrossMinutes)
	}
	if sum.NetMinutes != 457 {
		t.Errorf("net: got %d, want 457", sum.NetMinutes)
	}
	// 457 / 30 = 15 intervals = 450 min reported
	if sum.ReportedMinutes != 450 {
		t.Errorf("reported: got %d, want 450", sum.ReportedMinutes)
	}
	if sum.ReportedHours != 7.5 {
		t.Errorf("reported hours: got %g, want 7.5", sum.ReportedHours)
	}
	if sum.RemainderMinutes != 7 {
		t.Errorf("remainder: got %d, want 7", sum.RemainderMinutes)
	}
}

func TestCalculateDay_MultipleSessions(t *testing.T) {
	settings := DefaultSettings()
	day := WorkDay{
		Date:  "2026-03-27",
		Lunch: false,
		Sessions: []WorkSession{
			{StartTime: "06:00", EndTime: "08:00"}, // 120 min
			{StartTime: "08:45", EndTime: "15:00"}, // 375 min
		},
	}

	sum, err := CalculateDay(day, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sum.GrossMinutes != 495 {
		t.Errorf("gross: got %d, want 495", sum.GrossMinutes)
	}
	if sum.LunchMinutes != 0 {
		t.Errorf("lunch: got %d, want 0 (no lunch checked)", sum.LunchMinutes)
	}
	if sum.NetMinutes != 495 {
		t.Errorf("net: got %d, want 495", sum.NetMinutes)
	}
	// 495 / 30 = 16 intervals = 480 reported, remainder 15
	if sum.ReportedMinutes != 480 {
		t.Errorf("reported: got %d, want 480", sum.ReportedMinutes)
	}
	if sum.RemainderMinutes != 15 {
		t.Errorf("remainder: got %d, want 15", sum.RemainderMinutes)
	}
}

func TestCalculateDay_WithCustomDeductions(t *testing.T) {
	settings := DefaultSettings()
	day := WorkDay{
		Date:  "2026-03-27",
		Lunch: true,
		Sessions: []WorkSession{
			{StartTime: "06:00", EndTime: "15:00"}, // 540 min
		},
		Deductions: []WorkDeduction{
			{Name: "Kindergarten", Minutes: 15},
		},
	}

	sum, err := CalculateDay(day, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// net = 540 - 30 - 15 = 495
	if sum.NetMinutes != 495 {
		t.Errorf("net: got %d, want 495", sum.NetMinutes)
	}
	if sum.DeductionMinutes != 15 {
		t.Errorf("custom deductions: got %d, want 15", sum.DeductionMinutes)
	}
}

func TestCalculateDay_NoSessions(t *testing.T) {
	settings := DefaultSettings()
	day := WorkDay{
		Date:     "2026-03-27",
		Lunch:    false,
		Sessions: []WorkSession{},
	}

	sum, err := CalculateDay(day, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sum.GrossMinutes != 0 || sum.NetMinutes != 0 || sum.ReportedMinutes != 0 {
		t.Errorf("expected all zeros for empty day, got gross=%d net=%d reported=%d",
			sum.GrossMinutes, sum.NetMinutes, sum.ReportedMinutes)
	}
}

func TestCalculateDay_NetClampedToZero(t *testing.T) {
	// Deductions exceed gross: net should be clamped to 0.
	settings := DefaultSettings()
	day := WorkDay{
		Date:  "2026-03-27",
		Lunch: true,
		Sessions: []WorkSession{
			{StartTime: "09:00", EndTime: "09:20"}, // 20 min
		},
	}

	sum, err := CalculateDay(day, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sum.NetMinutes != 0 {
		t.Errorf("net: got %d, want 0 (clamped)", sum.NetMinutes)
	}
}

func TestCalculateFlexPool(t *testing.T) {
	summaries := []DaySummary{
		{RemainderMinutes: 7},
		{RemainderMinutes: 8},
		{RemainderMinutes: 12},
		{RemainderMinutes: 15},
		{RemainderMinutes: 0},
	}

	result := CalculateFlexPool(summaries, 30)
	if result.TotalMinutes != 42 {
		t.Errorf("total: got %d, want 42", result.TotalMinutes)
	}
	// 42 % 30 = 12, to_next = 30 - 12 = 18
	if result.ToNextInterval != 18 {
		t.Errorf("to_next: got %d, want 18", result.ToNextInterval)
	}
}

func TestCalculateFlexPool_Empty(t *testing.T) {
	result := CalculateFlexPool(nil, 30)
	if result.TotalMinutes != 0 {
		t.Errorf("total: got %d, want 0", result.TotalMinutes)
	}
	if result.ToNextInterval != 0 {
		t.Errorf("to_next: got %d, want 0", result.ToNextInterval)
	}
}

func TestCalculateFlexPool_ExactMultiple(t *testing.T) {
	summaries := []DaySummary{
		{RemainderMinutes: 30},
	}
	result := CalculateFlexPool(summaries, 30)
	if result.TotalMinutes != 30 {
		t.Errorf("total: got %d, want 30", result.TotalMinutes)
	}
	if result.ToNextInterval != 0 {
		t.Errorf("to_next: got %d, want 0 (exact multiple)", result.ToNextInterval)
	}
}

func TestParseHHMM(t *testing.T) {
	tests := []struct {
		input   string
		wantMin int
		wantErr bool
	}{
		{"00:00", 0, false},
		{"06:00", 360, false},
		{"23:59", 1439, false},
		{"08:30", 510, false},
		{"invalid", 0, true},
		{"25:00", 0, true},
		{"08:60", 0, true},
	}

	for _, tc := range tests {
		got, err := parseHHMM(tc.input)
		if tc.wantErr && err == nil {
			t.Errorf("parseHHMM(%q): expected error, got none", tc.input)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("parseHHMM(%q): unexpected error: %v", tc.input, err)
		}
		if !tc.wantErr && got != tc.wantMin {
			t.Errorf("parseHHMM(%q): got %d, want %d", tc.input, got, tc.wantMin)
		}
	}
}

func TestSessionMinutes_CrossesMidnight(t *testing.T) {
	tests := []struct {
		name    string
		session WorkSession
		want    int
	}{
		{
			name:    "wrapped 22:00 to 02:00 is four hours",
			session: WorkSession{StartTime: "22:00", EndTime: "02:00", CrossesMidnight: true},
			want:    240,
		},
		{
			name:    "wrapped session ending at midnight",
			session: WorkSession{StartTime: "23:30", EndTime: "00:00", CrossesMidnight: true},
			want:    30,
		},
		{
			name:    "same times unwrapped still yields zero",
			session: WorkSession{StartTime: "22:00", EndTime: "02:00"},
			want:    0,
		},
		{
			name:    "ordinary session is unaffected",
			session: WorkSession{StartTime: "08:00", EndTime: "16:00"},
			want:    480,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sessionMinutes(tc.session)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("sessionMinutes: got %d, want %d", got, tc.want)
			}
		})
	}
}

// SessionDurationMinutes is the shared entry point used by other packages (for
// example salary) that roll up work hours, so it is tested directly.
func TestSessionDurationMinutes(t *testing.T) {
	tests := []struct {
		name            string
		start, end      string
		crossesMidnight bool
		want            int
		wantErr         bool
	}{
		{name: "ordinary session", start: "08:00", end: "16:00", want: 480},
		{name: "wrapped session", start: "22:00", end: "02:00", crossesMidnight: true, want: 240},
		{name: "wrapped session ending at midnight", start: "23:30", end: "00:00", crossesMidnight: true, want: 30},
		{name: "unflagged wrapped range counts as zero", start: "22:00", end: "02:00", want: 0},
		{name: "invalid start time", start: "nope", end: "16:00", wantErr: true},
		{name: "invalid end time", start: "08:00", end: "25:00", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SessionDurationMinutes(tc.start, tc.end, tc.crossesMidnight)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("SessionDurationMinutes(%q, %q, %v): expected error", tc.start, tc.end, tc.crossesMidnight)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("SessionDurationMinutes: got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCalculateDay_CrossesMidnightCountsOnStartDate(t *testing.T) {
	settings := DefaultSettings()
	day := WorkDay{
		Date: "2026-03-27",
		Sessions: []WorkSession{
			{StartTime: "22:00", EndTime: "02:00", CrossesMidnight: true}, // 240 min
		},
	}

	sum, err := CalculateDay(day, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sum.Date != "2026-03-27" {
		t.Errorf("date: got %q, want the punch-in date", sum.Date)
	}
	if sum.GrossMinutes != 240 {
		t.Errorf("gross: got %d, want 240", sum.GrossMinutes)
	}
	if sum.NetMinutes != 240 {
		t.Errorf("net: got %d, want 240", sum.NetMinutes)
	}
	// 240 is an exact multiple of the 30-minute rounding.
	if sum.ReportedMinutes != 240 {
		t.Errorf("reported: got %d, want 240", sum.ReportedMinutes)
	}
	if sum.RemainderMinutes != 0 {
		t.Errorf("remainder: got %d, want 0", sum.RemainderMinutes)
	}
}

func TestCalculateDay_MixedWrappedAndNormalSessions(t *testing.T) {
	settings := DefaultSettings()
	day := WorkDay{
		Date:  "2026-03-27",
		Lunch: true,
		Sessions: []WorkSession{
			{StartTime: "09:00", EndTime: "17:00"},                        // 480 min
			{StartTime: "22:00", EndTime: "01:15", CrossesMidnight: true}, // 195 min
		},
	}

	sum, err := CalculateDay(day, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The wrapped minutes are counted exactly once, on the start date.
	if sum.GrossMinutes != 675 {
		t.Errorf("gross: got %d, want 675", sum.GrossMinutes)
	}
	if sum.NetMinutes != 645 {
		t.Errorf("net: got %d, want 645", sum.NetMinutes)
	}
	if sum.ReportedMinutes != 630 {
		t.Errorf("reported: got %d, want 630", sum.ReportedMinutes)
	}
	if sum.RemainderMinutes != 15 {
		t.Errorf("remainder: got %d, want 15", sum.RemainderMinutes)
	}
}

func TestValidateSessionTimes(t *testing.T) {
	tests := []struct {
		name            string
		start, end      string
		crossesMidnight bool
		wantErr         bool
	}{
		{name: "normal session", start: "09:00", end: "17:00"},
		{name: "normal session with end before start", start: "17:00", end: "09:00", wantErr: true},
		{name: "normal session with equal times", start: "09:00", end: "09:00", wantErr: true},
		{name: "wrapped session", start: "22:00", end: "02:00", crossesMidnight: true},
		{name: "wrapped session with end after start", start: "09:00", end: "17:00", crossesMidnight: true, wantErr: true},
		{name: "wrapped session with equal times would be 24h", start: "22:00", end: "22:00", crossesMidnight: true, wantErr: true},
		{name: "invalid start", start: "nope", end: "17:00", wantErr: true},
		{name: "invalid end", start: "09:00", end: "99:99", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSessionTimes(tc.start, tc.end, tc.crossesMidnight)
			if tc.wantErr && err == nil {
				t.Error("expected an error, got none")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
