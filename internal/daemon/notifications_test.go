package daemon

import (
	"testing"
	"time"
)

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
