package suggestions

import (
	"testing"
	"time"
)

func mustLoadOslo(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Fatalf("load Europe/Oslo: %v", err)
	}
	return loc
}

func TestNextScheduledRun_BasicSameAndNextDay(t *testing.T) {
	oslo := mustLoadOslo(t)

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "before 03:00 returns same day 03:00",
			now:  time.Date(2026, 4, 6, 1, 0, 0, 0, oslo),
			want: time.Date(2026, 4, 6, 3, 0, 0, 0, oslo),
		},
		{
			name: "after 03:00 returns next day 03:00",
			now:  time.Date(2026, 4, 6, 4, 0, 0, 0, oslo),
			want: time.Date(2026, 4, 7, 3, 0, 0, 0, oslo),
		},
		{
			name: "exactly 03:00 returns next day",
			now:  time.Date(2026, 4, 6, 3, 0, 0, 0, oslo),
			want: time.Date(2026, 4, 7, 3, 0, 0, 0, oslo),
		},
		{
			name: "nil location defaults to UTC",
			now:  time.Date(2026, 4, 6, 1, 0, 0, 0, time.UTC),
			want: time.Date(2026, 4, 6, 3, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var loc *time.Location
			if tc.name == "nil location defaults to UTC" {
				loc = nil
			} else {
				loc = oslo
			}
			got := NextScheduledRun(tc.now, loc)
			if !got.Equal(tc.want) {
				t.Errorf("NextScheduledRun(%v) = %v, want %v", tc.now, got, tc.want)
			}
			if !got.After(tc.now) {
				t.Errorf("result %v is not after now %v", got, tc.now)
			}
			if h := got.In(loc).Hour(); loc != nil && h != 3 {
				t.Errorf("expected hour 3 in loc, got %d", h)
			}
		})
	}
}

// TestNextScheduledRun_SpringForward verifies behaviour around the European
// spring-forward DST transition (last Sunday of March). On 2026-03-29 the
// Oslo clock jumps from 01:59:59 CET (UTC+1) to 03:00:00 CEST (UTC+2).
func TestNextScheduledRun_SpringForward(t *testing.T) {
	oslo := mustLoadOslo(t)

	t.Run("before 03:00 on transition day returns today 03:00 CEST", func(t *testing.T) {
		now := time.Date(2026, 3, 29, 1, 0, 0, 0, oslo) // 01:00 CET (UTC+1)
		got := NextScheduledRun(now, oslo)
		want := time.Date(2026, 3, 29, 3, 0, 0, 0, oslo) // 03:00 CEST (UTC+2)
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
		if h := got.In(oslo).Hour(); h != 3 {
			t.Errorf("expected wall-clock hour 3, got %d", h)
		}
		if _, off := got.Zone(); off != 2*3600 {
			t.Errorf("expected UTC offset +7200 (CEST), got %d", off)
		}
	})

	t.Run("after transition returns next day 03:00 CEST", func(t *testing.T) {
		now := time.Date(2026, 3, 29, 4, 0, 0, 0, oslo) // 04:00 CEST
		got := NextScheduledRun(now, oslo)
		want := time.Date(2026, 3, 30, 3, 0, 0, 0, oslo)
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
		if h := got.In(oslo).Hour(); h != 3 {
			t.Errorf("expected wall-clock hour 3, got %d", h)
		}
		if _, off := got.Zone(); off != 2*3600 {
			t.Errorf("expected UTC offset +7200 (CEST), got %d", off)
		}
	})
}

// TestNextScheduledRun_FallBack verifies behaviour around the European
// fall-back DST transition (last Sunday of October). On 2026-10-25 the Oslo
// clock turns back from 02:59:59 CEST (UTC+2) to 02:00:00 CET (UTC+1); the
// 03:00 wall-clock instant on that day is CET.
func TestNextScheduledRun_FallBack(t *testing.T) {
	oslo := mustLoadOslo(t)

	t.Run("before 03:00 on transition day returns today 03:00 CET", func(t *testing.T) {
		now := time.Date(2026, 10, 25, 1, 0, 0, 0, oslo) // 01:00 CEST (UTC+2)
		got := NextScheduledRun(now, oslo)
		want := time.Date(2026, 10, 25, 3, 0, 0, 0, oslo) // 03:00 CET (UTC+1)
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
		if h := got.In(oslo).Hour(); h != 3 {
			t.Errorf("expected wall-clock hour 3, got %d", h)
		}
		if _, off := got.Zone(); off != 1*3600 {
			t.Errorf("expected UTC offset +3600 (CET), got %d", off)
		}
		if !got.After(now) {
			t.Errorf("result %v not after now %v", got, now)
		}
	})

	t.Run("after 03:00 on transition day returns next day 03:00 CET", func(t *testing.T) {
		now := time.Date(2026, 10, 25, 4, 0, 0, 0, oslo) // 04:00 CET (UTC+1)
		got := NextScheduledRun(now, oslo)
		want := time.Date(2026, 10, 26, 3, 0, 0, 0, oslo)
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
		if h := got.In(oslo).Hour(); h != 3 {
			t.Errorf("expected wall-clock hour 3, got %d", h)
		}
		if _, off := got.Zone(); off != 1*3600 {
			t.Errorf("expected UTC offset +3600 (CET), got %d", off)
		}
	})
}
