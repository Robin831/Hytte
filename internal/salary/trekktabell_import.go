package salary

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// skatteetatenLineLen is the content length (excluding CR/LF) of each row in
// the skatteetaten fixed-width file. Layout:
//
//	chars 0-3   table number    (e.g. "8050")
//	chars 4-5   period code     ("10"=month, "20"=14-day, "30"=week, ...)
//	chars 6-11  income (NOK)    (zero-padded, integer)
//	chars 12-17 tax   (NOK)     (zero-padded, integer)
const skatteetatenLineLen = 18

// skatteetatenMonthlyPeriodCode identifies monthly rows in the file. We only
// import this subset; other period codes (14-day, weekly, daily) are ignored.
const skatteetatenMonthlyPeriodCode = "10"

// ParseSkatteetatenMonthly parses the skatteetaten fixed-width trekktabell
// file, filters to monthly rows (period code "10"), and tags each row with
// the given year (the file has no year field — the caller supplies it based
// on which file they downloaded).
//
// Lines shorter than 18 characters, blank lines, and rows with non-monthly
// period codes are silently skipped. Any row that has the correct length but
// contains non-digit characters in the numeric fields is a hard parse error,
// because that indicates a file format we don't recognise.
func ParseSkatteetatenMonthly(r io.Reader, year int) ([]TrekktabellRow, error) {
	if year < 2000 || year > 2100 {
		return nil, fmt.Errorf("invalid year %d: expected 2000-2100", year)
	}
	scanner := bufio.NewScanner(r)
	// The file is ~21 MB with ~1 million short lines, so bufio's default
	// 64 KB buffer is more than enough — no need to grow it.
	var rows []TrekktabellRow
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimRight(scanner.Text(), "\r\n ")
		if len(line) == 0 {
			continue
		}
		if len(line) != skatteetatenLineLen {
			continue
		}
		if line[4:6] != skatteetatenMonthlyPeriodCode {
			continue
		}
		tableNumber := line[0:4]
		if !allDigits(tableNumber) {
			return nil, fmt.Errorf("line %d: table number %q is not numeric", lineNum, tableNumber)
		}
		income, err := strconv.Atoi(line[6:12])
		if err != nil {
			return nil, fmt.Errorf("line %d: parse income %q: %w", lineNum, line[6:12], err)
		}
		tax, err := strconv.Atoi(line[12:18])
		if err != nil {
			return nil, fmt.Errorf("line %d: parse tax %q: %w", lineNum, line[12:18], err)
		}
		rows = append(rows, TrekktabellRow{
			TableNumber: tableNumber,
			Year:        year,
			Income:      income,
			Tax:         tax,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan file: %w", err)
	}
	return rows, nil
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}
