package races

import (
	"fmt"
	"strings"
	"time"
)

// Push notifications are rendered server-side in the recipient's language
// (the ui_language preference the web app keeps in sync). These strings mirror
// web/public/locales/*/races.json; keep the two in step.

type pushStrings struct {
	kinds      map[string]string
	statuses   map[string]string
	entryTypes map[string]string
	months     [12]string
	weekdays   [7]string // Sunday first, like time.Weekday
	today      string
	tomorrow   string
	inDays     func(n int) string
	around     string // "around {date}"
	expected   string // "expected {fuzzy date}"
	precision  map[string]string
	dayMonth   func(day int, month string, year int, withYear bool) string
	at         func(date, clock string) string
	status     string
	raceDate   string
	entry      string
	isNew      string
	removed    string
}

var pushText = map[string]pushStrings{
	"en": {
		kinds: map[string]string{
			"entry_opens": "Entry opens", "entry_closes": "Entry closes", "lottery_opens": "Lottery opens",
			"lottery_closes": "Lottery closes", "lottery_results": "Lottery results", "payment_due": "Payment due",
			"price_increase": "Price goes up", "waitlist_closes": "Waitlist closes", "other": "Note",
		},
		statuses:   map[string]string{"open": "Open now", "later": "Later or unclear", "closed": "Closed"},
		entryTypes: map[string]string{"lottery": "Lottery", "fcfs": "First come, first served", "qualifier": "Qualifying time", "unknown": "Unknown"},
		months:     [12]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
		weekdays:   [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
		today:      "today",
		tomorrow:   "tomorrow",
		inDays:     func(n int) string { return fmt.Sprintf("in %d days", n) },
		around:     "around %s",
		expected:   "expected %s",
		precision:  map[string]string{"early": "early %s", "mid": "mid %s", "late": "late %s", "month": "%s"},
		dayMonth: func(day int, month string, year int, withYear bool) string {
			if withYear {
				return fmt.Sprintf("%d %s %d", day, month, year)
			}
			return fmt.Sprintf("%d %s", day, month)
		},
		at:       func(date, clock string) string { return date + " at " + clock },
		status:   "Status",
		raceDate: "Race date",
		entry:    "Entry",
		isNew:    "new",
		removed:  "removed",
	},
	"nb": {
		kinds: map[string]string{
			"entry_opens": "Påmeldingen åpner", "entry_closes": "Påmeldingen stenger", "lottery_opens": "Trekningen åpner",
			"lottery_closes": "Trekningen stenger", "lottery_results": "Trekningsresultat", "payment_due": "Betalingsfrist",
			"price_increase": "Prisen øker", "waitlist_closes": "Ventelisten stenger", "other": "Merknad",
		},
		statuses:   map[string]string{"open": "Åpen nå", "later": "Senere eller uklar", "closed": "Stengt"},
		entryTypes: map[string]string{"lottery": "Trekning", "fcfs": "Først til mølla", "qualifier": "Kvalifiseringstid", "unknown": "Ukjent"},
		months:     [12]string{"jan.", "feb.", "mars", "apr.", "mai", "juni", "juli", "aug.", "sep.", "okt.", "nov.", "des."},
		weekdays:   [7]string{"søn.", "man.", "tir.", "ons.", "tor.", "fre.", "lør."},
		today:      "i dag",
		tomorrow:   "i morgen",
		inDays:     func(n int) string { return fmt.Sprintf("om %d dager", n) },
		around:     "rundt %s",
		expected:   "forventet %s",
		precision:  map[string]string{"early": "tidlig i %s", "mid": "midt i %s", "late": "sent i %s", "month": "%s"},
		dayMonth: func(day int, month string, year int, withYear bool) string {
			if withYear {
				return fmt.Sprintf("%d. %s %d", day, month, year)
			}
			return fmt.Sprintf("%d. %s", day, month)
		},
		at:       func(date, clock string) string { return date + " kl. " + clock },
		status:   "Status",
		raceDate: "Løpsdato",
		entry:    "Påmelding",
		isNew:    "ny",
		removed:  "fjernet",
	},
	"th": {
		kinds: map[string]string{
			"entry_opens": "เปิดรับสมัคร", "entry_closes": "ปิดรับสมัคร", "lottery_opens": "เปิดจับฉลาก",
			"lottery_closes": "ปิดจับฉลาก", "lottery_results": "ประกาศผลจับฉลาก", "payment_due": "กำหนดชำระเงิน",
			"price_increase": "ราคาขึ้น", "waitlist_closes": "ปิดรายชื่อสำรอง", "other": "หมายเหตุ",
		},
		statuses:   map[string]string{"open": "เปิดรับอยู่", "later": "เปิดภายหลังหรือยังไม่ชัดเจน", "closed": "ปิดแล้ว"},
		entryTypes: map[string]string{"lottery": "จับฉลาก", "fcfs": "มาก่อนได้ก่อน", "qualifier": "ต้องมีเวลาผ่านเกณฑ์", "unknown": "ไม่ทราบ"},
		months:     [12]string{"ม.ค.", "ก.พ.", "มี.ค.", "เม.ย.", "พ.ค.", "มิ.ย.", "ก.ค.", "ส.ค.", "ก.ย.", "ต.ค.", "พ.ย.", "ธ.ค."},
		weekdays:   [7]string{"อา.", "จ.", "อ.", "พ.", "พฤ.", "ศ.", "ส."},
		today:      "วันนี้",
		tomorrow:   "พรุ่งนี้",
		inDays:     func(n int) string { return fmt.Sprintf("อีก %d วัน", n) },
		around:     "ประมาณ %s",
		expected:   "คาดว่า %s",
		precision:  map[string]string{"early": "ต้น%s", "mid": "กลาง%s", "late": "ปลาย%s", "month": "%s"},
		dayMonth: func(day int, month string, year int, withYear bool) string {
			if withYear {
				return fmt.Sprintf("%d %s %d", day, month, year)
			}
			return fmt.Sprintf("%d %s", day, month)
		},
		at:       func(date, clock string) string { return date + " เวลา " + clock + " น." },
		status:   "สถานะ",
		raceDate: "วันแข่ง",
		entry:    "การสมัคร",
		isNew:    "ใหม่",
		removed:  "ยกเลิกแล้ว",
	},
}

func stringsFor(lang string) pushStrings {
	if s, ok := pushText[lang]; ok {
		return s
	}
	return pushText["en"]
}

// formatDay renders a calendar date ("31 Oct", "31. okt."), adding the year
// when it isn't the current one.
func (s pushStrings) formatDay(d time.Time, now time.Time) string {
	return s.dayMonth(d.Day(), s.months[d.Month()-1], d.Year(), d.Year() != now.Year())
}

// formatInstant renders an exact moment in loc with weekday and clock time.
func (s pushStrings) formatInstant(t time.Time, loc *time.Location, now time.Time) string {
	lt := t.In(loc)
	return s.at(s.weekdays[lt.Weekday()]+" "+s.formatDay(lt, now.In(loc)), lt.Format("15:04"))
}

// formatFuzzy renders a date honoring its precision ("early Dec 2026").
func (s pushStrings) formatFuzzy(d time.Time, precision string, now time.Time) string {
	switch precision {
	case "early", "mid", "late", "month":
		monthYear := s.months[d.Month()-1] + " " + fmt.Sprint(d.Year())
		return fmt.Sprintf(s.precision[precision], monthYear)
	case "approx":
		return fmt.Sprintf(s.around, s.formatDay(d, now))
	default:
		return s.formatDay(d, now)
	}
}

// relativeDays is "today" / "tomorrow" / "in 5 days (31 Oct)".
func (s pushStrings) relativeDays(n int, d time.Time, now time.Time) string {
	switch {
	case n <= 0:
		return s.today
	case n == 1:
		return s.tomorrow
	default:
		return s.inDays(n) + " (" + s.formatDay(d, now) + ")"
	}
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max-1])) + "…"
}
