// Package doomsday stores practice results for the weekday-of-a-date
// trainer (Conway's Doomsday method) and serves stats per level. The method
// itself, the dates and the hints live in the frontend; the server checks
// each result against the real calendar, so stats can't be fooled by a
// client bug.
package doomsday

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"sort"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

var (
	errInvalid = errors.New("invalid")
	isoDate    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	validSlips = map[string]bool{"": true, "leap": true, "anchor": true, "firstEleven": true, "secondEleven": true,
		"noSubtract": true, "month": true, "offByOne": true, "unknown": true, "timeout": true}
	validDrills = map[string]bool{"": true, "anchor": true, "year": true, "month": true, "count": true}
)

// Attempt is one answered date.
type Attempt struct {
	Level      int    `json:"level"`
	Drill      string `json:"drill"`
	Date       string `json:"date"`
	Answer     int    `json:"answer"` // 0 = Sunday … 6 = Saturday; -1 = time ran out
	ResponseMS int    `json:"response_ms"`
	Hints      int    `json:"hints"`
	Slip       string `json:"slip"`
	Correct    bool   `json:"correct"`
	CreatedAt  string `json:"created_at,omitempty"`
}

// weekday is the true weekday of an ISO date, Sunday = 0.
func weekday(date string) (int, error) {
	if !isoDate.MatchString(date) {
		return 0, errInvalid
	}
	t, err := time.Parse("2006-01-02", date)
	if err != nil || t.Year() < 1500 || t.Year() > 2500 {
		return 0, errInvalid
	}
	return int(t.Weekday()), nil
}

// Record stores an attempt; correctness comes from the calendar. Drill
// attempts (level 2) record the step's answer as the client judged it.
func Record(ctx context.Context, db *sql.DB, userID int64, a Attempt) (Attempt, error) {
	if a.Level < 1 || a.Level > 5 || !validDrills[a.Drill] || !validSlips[a.Slip] ||
		a.Answer < -1 || a.Answer > 6 || (a.Answer == -1) != (a.Slip == "timeout") || a.ResponseMS < 0 || a.ResponseMS > 3_600_000 || a.Hints < 0 || a.Hints > 10 {
		return a, errInvalid
	}
	wd, err := weekday(a.Date)
	if err != nil {
		return a, err
	}
	if a.Drill == "" || a.Answer == -1 {
		a.Correct = wd == a.Answer
	}
	if a.Correct {
		a.Slip = ""
	}
	_, err = db.ExecContext(ctx, `INSERT INTO doomsday_attempts (user_id, level, drill, date, answer, correct, response_ms, hints, slip, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, userID, a.Level, a.Drill, a.Date, a.Answer, a.Correct, a.ResponseMS, a.Hints, a.Slip, time.Now().UTC())
	return a, err
}

// LevelStats summarises one level.
type LevelStats struct {
	Level      int     `json:"level"`
	Attempts   int     `json:"attempts"`
	Correct    int     `json:"correct"`
	Accuracy   float64 `json:"accuracy"`
	MedianMS   int     `json:"median_ms"` // over correct answers
	BestMS     int     `json:"best_ms"`   // fastest correct answer without hints
	Streak     int     `json:"streak"`    // current run of correct answers
	BestStreak int     `json:"best_streak"`
}

// Stats is the GET /api/math/doomsday/stats response.
type Stats struct {
	Levels []LevelStats   `json:"levels"`
	Slips  map[string]int `json:"slips"` // wrong answers by likely slip
	Recent []Attempt      `json:"recent"`
	Today  map[string]int `json:"today"` // attempts and correct today
	Daily  []DayCount     `json:"daily"` // last 30 days
}

// DayCount is one day's practice.
type DayCount struct {
	Day      string `json:"day"`
	Attempts int    `json:"attempts"`
	Correct  int    `json:"correct"`
}

// LoadStats computes the user's stats.
func LoadStats(ctx context.Context, db *sql.DB, userID int64) (*Stats, error) {
	rows, err := db.QueryContext(ctx, `SELECT level, drill, date, answer, correct, response_ms, hints, slip, created_at
		FROM doomsday_attempts WHERE user_id = ? ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type acc struct {
		s     LevelStats
		times []int
		run   int
	}
	levels := map[int]*acc{}
	st := &Stats{Slips: map[string]int{}, Recent: []Attempt{}, Today: map[string]int{"attempts": 0, "correct": 0}, Daily: []DayCount{}}
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		oslo = time.UTC
	}
	today := time.Now().In(oslo).Format("2006-01-02")
	since := time.Now().In(oslo).AddDate(0, 0, -29).Format("2006-01-02")
	daily := map[string]*DayCount{}
	var all []Attempt
	for rows.Next() {
		var a Attempt
		var at time.Time
		if err := rows.Scan(&a.Level, &a.Drill, &a.Date, &a.Answer, &a.Correct, &a.ResponseMS, &a.Hints, &a.Slip, &at); err != nil {
			return nil, err
		}
		a.CreatedAt = at.UTC().Format(time.RFC3339)
		all = append(all, a)
		l := levels[a.Level]
		if l == nil {
			l = &acc{s: LevelStats{Level: a.Level}}
			levels[a.Level] = l
		}
		l.s.Attempts++
		if a.Correct {
			l.s.Correct++
			l.times = append(l.times, a.ResponseMS)
			if a.Hints == 0 && (l.s.BestMS == 0 || a.ResponseMS < l.s.BestMS) {
				l.s.BestMS = a.ResponseMS
			}
			l.run++
			if l.run > l.s.BestStreak {
				l.s.BestStreak = l.run
			}
		} else {
			l.run = 0
			if a.Slip != "" {
				st.Slips[a.Slip]++
			}
		}
		day := at.In(oslo).Format("2006-01-02")
		if day == today {
			st.Today["attempts"]++
			if a.Correct {
				st.Today["correct"]++
			}
		}
		if day >= since {
			d := daily[day]
			if d == nil {
				d = &DayCount{Day: day}
				daily[day] = d
			}
			d.Attempts++
			if a.Correct {
				d.Correct++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for lvl := 1; lvl <= 5; lvl++ {
		l := levels[lvl]
		if l == nil {
			continue
		}
		l.s.Streak = l.run
		if l.s.Attempts > 0 {
			l.s.Accuracy = float64(l.s.Correct) / float64(l.s.Attempts)
		}
		if n := len(l.times); n > 0 {
			sort.Ints(l.times)
			l.s.MedianMS = l.times[n/2]
		}
		st.Levels = append(st.Levels, l.s)
	}
	for i := len(all) - 1; i >= 0 && len(st.Recent) < 20; i-- {
		st.Recent = append(st.Recent, all[i])
	}
	for _, d := range daily {
		st.Daily = append(st.Daily, *d)
	}
	sort.Slice(st.Daily, func(i, j int) bool { return st.Daily[i].Day < st.Daily[j].Day })
	if st.Levels == nil {
		st.Levels = []LevelStats{}
	}
	return st, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("doomsday: encode: %v", err)
	}
}

// RegisterRoutes mounts the API; gate it with the "dommedag" feature.
func RegisterRoutes(r chi.Router, db *sql.DB) {
	r.Post("/math/doomsday/attempts", func(w http.ResponseWriter, r *http.Request) {
		var a Attempt
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		saved, err := Record(r.Context(), db, auth.UserFromContext(r.Context()).ID, a)
		if errors.Is(err, errInvalid) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid attempt"})
			return
		}
		if err != nil {
			log.Printf("doomsday: record: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save"})
			return
		}
		writeJSON(w, http.StatusCreated, saved)
	})
	r.Get("/math/doomsday/stats", func(w http.ResponseWriter, r *http.Request) {
		st, err := LoadStats(r.Context(), db, auth.UserFromContext(r.Context()).ID)
		if err != nil {
			log.Printf("doomsday: stats: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load stats"})
			return
		}
		writeJSON(w, http.StatusOK, st)
	})
}
