package budget

import "time"

// easterSunday computes Easter Sunday for the given year using the Meeus/Jones/Butcher algorithm.
func easterSunday(year int) time.Time {
	a := year % 19
	b := year / 100
	c := year % 100
	d := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	ii := c / 4
	k := c % 4
	l := (32 + 2*e + 2*ii - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := time.Month((h + l - 7*m + 114) / 31)
	day := ((h + l - 7*m + 114) % 31) + 1
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// norwegianHolidays returns a set of YYYY-MM-DD strings for Norwegian public holidays in the given year.
// It covers both fixed-date holidays and Easter-based movable holidays.
func norwegianHolidays(year int) map[string]bool {
	holidays := make(map[string]bool)
	add := func(t time.Time) {
		holidays[t.Format("2006-01-02")] = true
	}

	// Fixed public holidays
	add(time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC))   // Nyttårsdag
	add(time.Date(year, time.May, 1, 0, 0, 0, 0, time.UTC))       // Arbeidernes dag
	add(time.Date(year, time.May, 17, 0, 0, 0, 0, time.UTC))      // Grunnlovsdagen
	add(time.Date(year, time.December, 25, 0, 0, 0, 0, time.UTC)) // Første juledag
	add(time.Date(year, time.December, 26, 0, 0, 0, 0, time.UTC)) // Andre juledag

	// Easter-based movable holidays
	easter := easterSunday(year)
	add(easter.AddDate(0, 0, -3)) // Skjærtorsdag (Maundy Thursday)
	add(easter.AddDate(0, 0, -2)) // Langfredag (Good Friday)
	add(easter)                   // Første påskedag (Easter Sunday)
	add(easter.AddDate(0, 0, 1))  // Andre påskedag (Easter Monday)
	add(easter.AddDate(0, 0, 39)) // Kristi himmelfartsdag (Ascension Day)
	add(easter.AddDate(0, 0, 49)) // Første pinsedag (Whit Sunday)
	add(easter.AddDate(0, 0, 50)) // Andre pinsedag (Whit Monday)

	return holidays
}

// previousBusinessDay returns the last business day on or before t, skipping weekends
// and Norwegian public holidays by moving backwards. This is used for income dates:
// payday moves to the preceding Friday when it falls on a weekend or holiday.
func previousBusinessDay(t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())

	cachedYear := -1
	var holidays map[string]bool

	for {
		if d.Year() != cachedYear {
			holidays = norwegianHolidays(d.Year())
			cachedYear = d.Year()
		}
		wd := d.Weekday()
		if wd != time.Saturday && wd != time.Sunday && !holidays[d.Format("2006-01-02")] {
			return d
		}
		d = d.AddDate(0, 0, -1)
	}
}

// nextBusinessDay returns the first business day on or after t, skipping weekends
// and Norwegian public holidays.
func nextBusinessDay(t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())

	cachedYear := -1
	var holidays map[string]bool

	for {
		if d.Year() != cachedYear {
			holidays = norwegianHolidays(d.Year())
			cachedYear = d.Year()
		}
		wd := d.Weekday()
		if wd != time.Saturday && wd != time.Sunday && !holidays[d.Format("2006-01-02")] {
			return d
		}
		d = d.AddDate(0, 0, 1)
	}
}
