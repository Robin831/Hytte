package suggestions

import "time"

// NextScheduledRun returns the next time the nightly suggestion-rotation cron
// should fire (daily at 03:00 in the given location). If today's 03:00 is
// still in the future, today's run is returned; otherwise the next day's run
// is returned. The result is constructed via time.Date in loc on every call so
// DST transitions in the target zone are handled correctly.
func NextScheduledRun(now time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)
	todayRun := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, loc)
	if now.Before(todayRun) {
		return todayRun
	}
	tomorrow := now.AddDate(0, 0, 1)
	return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 3, 0, 0, 0, loc)
}
