package currency

import "time"

// NextDailyRun returns the next time the EUR/NOK sync should fire (daily at
// 06:00 in the given location). If today's 06:00 is still in the future, that
// instant is returned; otherwise the next day's 06:00 is returned. The result
// is constructed via time.Date in loc on every call so DST transitions in the
// target zone are handled correctly.
func NextDailyRun(now time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)
	todayRun := time.Date(now.Year(), now.Month(), now.Day(), 6, 0, 0, 0, loc)
	if now.Before(todayRun) {
		return todayRun
	}
	tomorrow := now.AddDate(0, 0, 1)
	return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 6, 0, 0, 0, loc)
}
