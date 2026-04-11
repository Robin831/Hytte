package salary

import (
	"strings"
	"testing"
)

func TestParseSkatteetatenMonthly_ParsesMonthlyRows(t *testing.T) {
	// Four rows, each 18 chars + CRLF, mirroring the real file layout:
	//   table | period | income  | tax
	//   "8050"  "10"    "040000"  "009258"   → monthly, should be kept
	//   "8010"  "10"    "040000"  "010096"   → monthly, should be kept
	//   "8050"  "20"    "020000"  "000500"   → 14-day, should be skipped
	//   "8050"  "10"    "200000"  "085640"   → monthly, should be kept
	input := "805010040000009258\r\n" +
		"801010040000010096\r\n" +
		"805020020000000500\r\n" +
		"805010200000085640\r\n"

	rows, err := ParseSkatteetatenMonthly(strings.NewReader(input), 2026)
	if err != nil {
		t.Fatalf("ParseSkatteetatenMonthly: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 monthly rows, got %d: %+v", len(rows), rows)
	}
	if rows[0] != (TrekktabellRow{TableNumber: "8050", Year: 2026, Income: 40000, Tax: 9258}) {
		t.Errorf("row 0 wrong: %+v", rows[0])
	}
	if rows[1] != (TrekktabellRow{TableNumber: "8010", Year: 2026, Income: 40000, Tax: 10096}) {
		t.Errorf("row 1 wrong: %+v", rows[1])
	}
	if rows[2] != (TrekktabellRow{TableNumber: "8050", Year: 2026, Income: 200000, Tax: 85640}) {
		t.Errorf("row 2 wrong: %+v", rows[2])
	}
}

func TestParseSkatteetatenMonthly_SkipsBlankAndShortLines(t *testing.T) {
	input := "\r\n" +
		"805010040000009258\r\n" +
		"short\r\n" +
		"\r\n" +
		"805010040100009296\r\n"

	rows, err := ParseSkatteetatenMonthly(strings.NewReader(input), 2026)
	if err != nil {
		t.Fatalf("ParseSkatteetatenMonthly: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestParseSkatteetatenMonthly_RejectsInvalidYear(t *testing.T) {
	for _, year := range []int{0, 1999, 2101} {
		if _, err := ParseSkatteetatenMonthly(strings.NewReader(""), year); err == nil {
			t.Errorf("year %d should have errored", year)
		}
	}
}

func TestParseSkatteetatenMonthly_HardFailsOnNonDigitNumericField(t *testing.T) {
	// Valid table + period + income + non-digit tax should fail loudly.
	input := "805010040000ABCDEF\r\n"
	if _, err := ParseSkatteetatenMonthly(strings.NewReader(input), 2026); err == nil {
		t.Error("expected parse error for non-digit tax field, got nil")
	}
}

func TestParseSkatteetatenMonthly_SkipsAllNonMonthlyPeriods(t *testing.T) {
	// period codes 20, 30, 40, 50, 60, 70 should all be filtered out.
	var b strings.Builder
	for _, code := range []string{"20", "30", "40", "50", "60", "70"} {
		b.WriteString("8050")
		b.WriteString(code)
		b.WriteString("040000009258\r\n")
	}
	rows, err := ParseSkatteetatenMonthly(strings.NewReader(b.String()), 2026)
	if err != nil {
		t.Fatalf("ParseSkatteetatenMonthly: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}
