package pokemon

import "time"

// NextWeeklySync returns the next time the weekly full sync should fire:
// Sunday 04:00 in the given location. Mirrors the pattern used by the
// allowance and Stride schedulers in cmd/server/main.go.
func NextWeeklySync(now time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)
	// time.Sunday == 0; days until Sunday = (7 - weekday) % 7.
	daysUntil := (7 - int(now.Weekday())) % 7
	if daysUntil == 0 {
		todayRun := time.Date(now.Year(), now.Month(), now.Day(), 4, 0, 0, 0, loc)
		if now.Before(todayRun) {
			return todayRun
		}
		return todayRun.AddDate(0, 0, 7)
	}
	return time.Date(now.Year(), now.Month(), now.Day()+daysUntil, 4, 0, 0, 0, loc)
}

// NextDailyPriceRefresh returns the next time the daily price-only sync
// should fire: 07:00 in the given location.
func NextDailyPriceRefresh(now time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)
	todayRun := time.Date(now.Year(), now.Month(), now.Day(), 7, 0, 0, 0, loc)
	if now.Before(todayRun) {
		return todayRun
	}
	tomorrow := now.AddDate(0, 0, 1)
	return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 7, 0, 0, 0, loc)
}
