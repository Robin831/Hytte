package workhours

import (
	"fmt"
	"strconv"
	"strings"
)

// parseHHMM parses a "HH:MM" string and returns total minutes since midnight.
func parseHHMM(t string) (int, error) {
	parts := strings.SplitN(t, ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid time format %q: expected HH:MM", t)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("invalid hour in time %q", t)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("invalid minute in time %q", t)
	}
	return h*60 + m, nil
}

// ValidateHHMM returns an error if the given string is not a valid HH:MM time.
func ValidateHHMM(t string) error {
	_, err := parseHHMM(t)
	return err
}

// sessionMinutes returns the duration of a session in minutes. Returns 0 if
// end is not after start (e.g. malformed entry).
func sessionMinutes(s WorkSession) (int, error) {
	start, err := parseHHMM(s.StartTime)
	if err != nil {
		return 0, fmt.Errorf("start_time: %w", err)
	}
	end, err := parseHHMM(s.EndTime)
	if err != nil {
		return 0, fmt.Errorf("end_time: %w", err)
	}
	return max(end-start, 0), nil
}

// CalculateDay computes gross/net/reported hours and remainder for a work day.
//
// Calculation:
//
//	gross   = sum of session durations
//	deduct  = lunch (if checked) + custom deductions
//	net     = gross - deduct  (clamped to 0)
//	reported = floor(net / rounding) * rounding
//	remainder = net - reported  → goes to flex pool
func CalculateDay(day WorkDay, settings UserSettings) (DaySummary, error) {
	rounding := settings.RoundingMinutes
	if rounding <= 0 {
		rounding = 30
	}

	gross := 0
	for _, s := range day.Sessions {
		min, err := sessionMinutes(s)
		if err != nil {
			return DaySummary{}, fmt.Errorf("session %d: %w", s.ID, err)
		}
		gross += min
	}

	lunchMin := 0
	if day.Lunch {
		lunchMin = settings.LunchMinutes
	}

	customMin := 0
	for _, d := range day.Deductions {
		customMin += d.Minutes
	}

	net := max(gross-lunchMin-customMin, 0)

	reportedMin := (net / rounding) * rounding
	reportedHours := float64(reportedMin) / 60.0
	remainder := net - reportedMin

	standard := settings.StandardDayMinutes
	balance := reportedMin - standard

	return DaySummary{
		Date:             day.Date,
		GrossMinutes:     gross,
		LunchMinutes:     lunchMin,
		DeductionMinutes: customMin,
		NetMinutes:       net,
		ReportedMinutes:  reportedMin,
		ReportedHours:    reportedHours,
		RemainderMinutes: remainder,
		StandardMinutes:  standard,
		BalanceMinutes:   balance,
	}, nil
}

// CalculateFlexPool sums the remainder minutes across all provided day
// summaries and returns the running total along with how many more minutes are
// needed to reach the next rounding threshold.
func CalculateFlexPool(summaries []DaySummary, rounding int) FlexPoolResult {
	if rounding <= 0 {
		rounding = 30
	}
	total := 0
	for _, s := range summaries {
		total += s.RemainderMinutes
	}

	toNext := 0
	if total > 0 {
		mod := total % rounding
		if mod != 0 {
			toNext = rounding - mod
		}
	}

	return FlexPoolResult{
		TotalMinutes:   total,
		ToNextInterval: toNext,
	}
}
