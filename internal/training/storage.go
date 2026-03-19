package training

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// List returns all workouts for a user (without samples), including tags.
func List(db *sql.DB, userID int64) ([]Workout, error) {
	rows, err := db.Query(`
		SELECT w.id, w.user_id, w.sport, w.title, w.started_at, w.duration_seconds,
		       w.distance_meters, w.avg_heart_rate, w.max_heart_rate,
		       w.avg_pace_sec_per_km, w.avg_cadence, w.calories,
		       w.ascent_meters, w.descent_meters, w.fit_file_hash, w.title_source, w.created_at,
		       (SELECT GROUP_CONCAT(tag) FROM (SELECT tag FROM workout_tags WHERE workout_id = w.id ORDER BY tag)) AS tags
		FROM workouts w
		WHERE w.user_id = ?
		GROUP BY w.id
		ORDER BY w.started_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list workouts: %w", err)
	}
	defer rows.Close()

	var workouts []Workout
	for rows.Next() {
		var w Workout
		var tagsStr sql.NullString
		if err := rows.Scan(
			&w.ID, &w.UserID, &w.Sport, &w.Title, &w.StartedAt,
			&w.DurationSeconds, &w.DistanceMeters, &w.AvgHeartRate,
			&w.MaxHeartRate, &w.AvgPaceSecPerKm, &w.AvgCadence,
			&w.Calories, &w.AscentMeters, &w.DescentMeters,
			&w.FitFileHash, &w.TitleSource, &w.CreatedAt, &tagsStr,
		); err != nil {
			return nil, fmt.Errorf("scan workout: %w", err)
		}
		if tagsStr.Valid && tagsStr.String != "" {
			w.Tags = strings.Split(tagsStr.String, ",")
		}
		workouts = append(workouts, w)
	}
	return workouts, rows.Err()
}

// GetByID returns a workout with laps, tags, and samples.
func GetByID(db *sql.DB, id, userID int64) (*Workout, error) {
	var w Workout
	err := db.QueryRow(`
		SELECT id, user_id, sport, title, started_at, duration_seconds,
		       distance_meters, avg_heart_rate, max_heart_rate,
		       avg_pace_sec_per_km, avg_cadence, calories,
		       ascent_meters, descent_meters, fit_file_hash, title_source, created_at
		FROM workouts
		WHERE id = ? AND user_id = ?`, id, userID).Scan(
		&w.ID, &w.UserID, &w.Sport, &w.Title, &w.StartedAt,
		&w.DurationSeconds, &w.DistanceMeters, &w.AvgHeartRate,
		&w.MaxHeartRate, &w.AvgPaceSecPerKm, &w.AvgCadence,
		&w.Calories, &w.AscentMeters, &w.DescentMeters,
		&w.FitFileHash, &w.TitleSource, &w.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	w.Laps, err = getLaps(db, w.ID)
	if err != nil {
		return nil, err
	}
	w.Tags, err = getTags(db, w.ID)
	if err != nil {
		return nil, err
	}
	w.Samples, err = getSamples(db, w.ID)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// Create stores a parsed workout and its related data in the database.
func Create(db *sql.DB, userID int64, pw *ParsedWorkout, hash string) (*Workout, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	startedAt := pw.StartedAt.UTC().Format(time.RFC3339)

	// Calculate avg pace.
	var avgPace float64
	if pw.DistanceMeters > 0 {
		avgPace = float64(pw.DurationSeconds) / (pw.DistanceMeters / 1000)
	}

	// Use FIT metadata title if available; otherwise derive from sport + date.
	title := pw.Title
	if title == "" {
		title = fmt.Sprintf("%s %s",
			capitalizeFirst(pw.Sport),
			pw.StartedAt.Format("2006-01-02 15:04"))
	}

	titleSource := "device"

	res, err := tx.Exec(`
		INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds,
		                      distance_meters, avg_heart_rate, max_heart_rate,
		                      avg_pace_sec_per_km, avg_cadence, calories,
		                      ascent_meters, descent_meters, fit_file_hash, title_source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, pw.Sport, title, startedAt, pw.DurationSeconds,
		pw.DistanceMeters, pw.AvgHeartRate, pw.MaxHeartRate,
		avgPace, pw.AvgCadence, pw.Calories,
		pw.AscentMeters, pw.DescentMeters, hash, titleSource, now)
	if err != nil {
		return nil, fmt.Errorf("insert workout: %w", err)
	}

	workoutID, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get workout id: %w", err)
	}

	// Insert laps.
	for i, lap := range pw.Laps {
		var avgPaceLap float64
		if lap.AvgSpeedMPerS > 0 {
			avgPaceLap = 1000 / lap.AvgSpeedMPerS
		}
		_, err = tx.Exec(`
			INSERT INTO workout_laps (workout_id, lap_number, start_offset_ms,
			                          duration_seconds, distance_meters,
			                          avg_heart_rate, max_heart_rate,
			                          avg_pace_sec_per_km, avg_cadence)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			workoutID, i+1, lap.StartOffsetMs,
			lap.DurationSeconds, lap.DistanceMeters,
			lap.AvgHeartRate, lap.MaxHeartRate,
			avgPaceLap, lap.AvgCadence)
		if err != nil {
			return nil, fmt.Errorf("insert lap %d: %w", i+1, err)
		}
	}

	// Insert samples as plain JSON.
	if len(pw.Samples) > 0 {
		samplesJSON, err := json.Marshal(pw.Samples)
		if err != nil {
			return nil, fmt.Errorf("marshal samples: %w", err)
		}
		_, err = tx.Exec(`INSERT INTO workout_samples (workout_id, data) VALUES (?, ?)`,
			workoutID, string(samplesJSON))
		if err != nil {
			return nil, fmt.Errorf("insert samples: %w", err)
		}
	}

	// Generate and insert auto-tags based on interval structure.
	autoTags := GenerateAutoTags(pw)
	for _, tag := range autoTags {
		_, err = tx.Exec(`INSERT OR IGNORE INTO workout_tags (workout_id, tag) VALUES (?, ?)`,
			workoutID, tag)
		if err != nil {
			return nil, fmt.Errorf("insert auto-tag: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return GetByID(db, workoutID, userID)
}

// Delete removes a workout and all related data.
func Delete(db *sql.DB, id, userID int64) error {
	res, err := db.Exec(`DELETE FROM workouts WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("delete workout: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpdateTags replaces manual tags for a workout, preserving auto-generated tags.
func UpdateTags(db *sql.DB, workoutID, userID int64, tags []string) error {
	// Verify ownership.
	var ownerID int64
	err := db.QueryRow(`SELECT user_id FROM workouts WHERE id = ?`, workoutID).Scan(&ownerID)
	if err != nil {
		return err
	}
	if ownerID != userID {
		return sql.ErrNoRows
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete only manual tags, leaving auto-tags and ai-tags untouched.
	_, err = tx.Exec(`DELETE FROM workout_tags WHERE workout_id = ? AND tag NOT GLOB 'auto:*' AND tag NOT GLOB 'ai:*'`, workoutID)
	if err != nil {
		return err
	}

	seen := make(map[string]bool)
	// Insert manual tags, filtering out any "auto:" prefix from user input.
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] || strings.HasPrefix(tag, "auto:") || strings.HasPrefix(tag, "ai:") {
			continue
		}
		seen[tag] = true
		_, err = tx.Exec(`INSERT OR IGNORE INTO workout_tags (workout_id, tag) VALUES (?, ?)`, workoutID, tag)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UpdateTitle updates the title of a workout and marks the source as 'user'.
func UpdateTitle(db *sql.DB, id, userID int64, title string) error {
	res, err := db.Exec(`UPDATE workouts SET title = ?, title_source = 'user' WHERE id = ? AND user_id = ?`,
		title, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetAITitle updates the workout title only if the user hasn't manually set one.
func SetAITitle(db *sql.DB, id, userID int64, title string) error {
	if title == "" {
		return nil
	}
	_, err := db.Exec(
		`UPDATE workouts
		 SET title = ?, title_source = 'ai'
		 WHERE id = ? AND user_id = ?
		   AND (
				title_source = 'ai'
				OR (
					(title_source IS NULL OR title_source = '')
					AND (title IS NULL OR title = '')
				)
		   )`,
		title, id, userID,
	)
	return err
}

// HashExists checks whether a workout with the given file hash already exists.
func HashExists(db *sql.DB, userID int64, hash string) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM workouts WHERE user_id = ? AND fit_file_hash = ?`,
		userID, hash).Scan(&count)
	return count > 0, err
}

// WeeklySummaries returns aggregated training volume per week.
func WeeklySummaries(db *sql.DB, userID int64) ([]WeeklySummary, error) {
	rows, err := db.Query(`
		SELECT
			strftime('%Y-%W', started_at) AS week,
			MIN(DATE(started_at, '-6 days', 'weekday 1')) AS week_start,
			SUM(duration_seconds) AS total_duration,
			SUM(distance_meters) AS total_distance,
			COUNT(*) AS workout_count,
			AVG(NULLIF(avg_heart_rate, 0)) AS avg_hr
		FROM workouts
		WHERE user_id = ?
		GROUP BY week
		ORDER BY week DESC
		LIMIT 52`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []WeeklySummary
	for rows.Next() {
		var s WeeklySummary
		var week string
		var avgHR sql.NullFloat64
		if err := rows.Scan(&week, &s.WeekStart, &s.TotalDuration, &s.TotalDistance, &s.WorkoutCount, &avgHR); err != nil {
			return nil, err
		}
		if avgHR.Valid {
			s.AvgHeartRate = avgHR.Float64
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

// GetProgression returns workouts grouped by tag for progression tracking.
func GetProgression(db *sql.DB, userID int64) ([]ProgressionGroup, error) {
	rows, err := db.Query(`
		SELECT w.id, w.sport, w.started_at, w.avg_heart_rate, w.avg_pace_sec_per_km,
		       t.tag,
		       (SELECT COUNT(*) FROM workout_laps l WHERE l.workout_id = w.id) AS lap_count
		FROM workouts w
		JOIN workout_tags t ON t.workout_id = w.id
		WHERE w.user_id = ?
		ORDER BY t.tag, w.started_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type progressionKey struct {
		tag, sport string
		lapCount   int
	}
	groups := make(map[progressionKey]*ProgressionGroup)
	for rows.Next() {
		var id int64
		var sport, startedAt, tag string
		var avgHR int
		var avgPace float64
		var lapCount int
		if err := rows.Scan(&id, &sport, &startedAt, &avgHR, &avgPace, &tag, &lapCount); err != nil {
			return nil, err
		}

		key := progressionKey{tag, sport, lapCount}
		g, ok := groups[key]
		if !ok {
			g = &ProgressionGroup{Tag: tag, Sport: sport, LapCount: lapCount}
			groups[key] = g
		}
		g.Workouts = append(g.Workouts, ProgressionPoint{
			WorkoutID: id,
			Date:      startedAt,
			AvgHR:     float64(avgHR),
			AvgPace:   avgPace,
		})
	}

	var result []ProgressionGroup
	for _, g := range groups {
		result = append(result, *g)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Tag != result[j].Tag {
			return result[i].Tag < result[j].Tag
		}
		if result[i].Sport != result[j].Sport {
			return result[i].Sport < result[j].Sport
		}
		return result[i].LapCount < result[j].LapCount
	})
	return result, rows.Err()
}

// checkOwnerAndGetSamples verifies ownership then fetches only the samples payload,
// avoiding the full GetByID (laps + tags + samples) for zone calculations.
func checkOwnerAndGetSamples(db *sql.DB, workoutID, userID int64) (*Samples, error) {
	var ownerID int64
	err := db.QueryRow(`SELECT user_id FROM workouts WHERE id = ?`, workoutID).Scan(&ownerID)
	if err != nil {
		return nil, err
	}
	if ownerID != userID {
		return nil, sql.ErrNoRows
	}
	return getSamples(db, workoutID)
}

// getWorkoutWithLaps fetches a workout with its laps but without tags or samples.
// Used for lightweight comparison and similarity operations.
func getWorkoutWithLaps(db *sql.DB, id, userID int64) (*Workout, error) {
	var w Workout
	err := db.QueryRow(`
		SELECT id, user_id, sport, title, started_at, duration_seconds,
		       distance_meters, avg_heart_rate, max_heart_rate,
		       avg_pace_sec_per_km, avg_cadence, calories,
		       ascent_meters, descent_meters, fit_file_hash, title_source, created_at
		FROM workouts
		WHERE id = ? AND user_id = ?`, id, userID).Scan(
		&w.ID, &w.UserID, &w.Sport, &w.Title, &w.StartedAt,
		&w.DurationSeconds, &w.DistanceMeters, &w.AvgHeartRate,
		&w.MaxHeartRate, &w.AvgPaceSecPerKm, &w.AvgCadence,
		&w.Calories, &w.AscentMeters, &w.DescentMeters,
		&w.FitFileHash, &w.TitleSource, &w.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	w.Laps, err = getLaps(db, w.ID)
	return &w, err
}

// GetZoneDistribution calculates HR zone distribution for a workout
// using threshold HR from lactate tests.
func GetZoneDistribution(db *sql.DB, workoutID, userID int64, thresholdHR int) ([]ZoneDistribution, error) {
	samples, err := checkOwnerAndGetSamples(db, workoutID, userID)
	if err != nil {
		return nil, err
	}
	if samples == nil || len(samples.Points) == 0 {
		return nil, nil
	}

	if thresholdHR <= 0 {
		thresholdHR = 180 // sensible default
	}

	// Define 5 zones based on threshold HR.
	zones := []struct {
		Zone  int
		Name  string
		MinPct float64
		MaxPct float64
	}{
		{1, "Recovery", 0, 0.72},
		{2, "Aerobic", 0.72, 0.82},
		{3, "Tempo", 0.82, 0.87},
		{4, "Threshold", 0.87, 0.92},
		{5, "VO2max", 0.92, 2.0},
	}

	dist := make([]ZoneDistribution, len(zones))
	for i, z := range zones {
		dist[i] = ZoneDistribution{
			Zone:  z.Zone,
			Name:  z.Name,
			MinHR: int(math.Round(float64(thresholdHR) * z.MinPct)),
			MaxHR: int(math.Round(float64(thresholdHR) * z.MaxPct)),
		}
	}

	// Compute zone durations from timestamp deltas between consecutive samples.
	// This correctly handles variable recording frequencies across devices/modes.
	points := samples.Points
	var totalSeconds float64
	for i, p := range points {
		if p.HeartRate <= 0 {
			continue
		}
		if i+1 >= len(points) {
			continue
		}
		durSec := float64(points[i+1].OffsetMs-p.OffsetMs) / 1000.0
		if durSec <= 0 {
			continue
		}
		totalSeconds += durSec
		ratio := float64(p.HeartRate) / float64(thresholdHR)
		for zi, z := range zones {
			if ratio >= z.MinPct && ratio < z.MaxPct {
				dist[zi].DurationS += durSec
				break
			}
		}
	}

	if totalSeconds > 0 {
		for i := range dist {
			dist[i].Percentage = (dist[i].DurationS / totalSeconds) * 100
		}
	}

	return dist, nil
}

func getLaps(db *sql.DB, workoutID int64) ([]Lap, error) {
	rows, err := db.Query(`
		SELECT id, workout_id, lap_number, start_offset_ms, duration_seconds,
		       distance_meters, avg_heart_rate, max_heart_rate,
		       avg_pace_sec_per_km, avg_cadence
		FROM workout_laps
		WHERE workout_id = ?
		ORDER BY lap_number`, workoutID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var laps []Lap
	for rows.Next() {
		var l Lap
		if err := rows.Scan(&l.ID, &l.WorkoutID, &l.LapNumber, &l.StartOffsetMs,
			&l.DurationSeconds, &l.DistanceMeters, &l.AvgHeartRate, &l.MaxHeartRate,
			&l.AvgPaceSecPerKm, &l.AvgCadence); err != nil {
			return nil, err
		}
		laps = append(laps, l)
	}
	return laps, rows.Err()
}

func getTags(db *sql.DB, workoutID int64) ([]string, error) {
	rows, err := db.Query(`SELECT tag FROM workout_tags WHERE workout_id = ? ORDER BY tag`, workoutID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func getSamples(db *sql.DB, workoutID int64) (*Samples, error) {
	var data string
	err := db.QueryRow(`SELECT data FROM workout_samples WHERE workout_id = ?`, workoutID).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var points []Sample
	if err := json.Unmarshal([]byte(data), &points); err != nil {
		return nil, fmt.Errorf("unmarshal samples: %w", err)
	}
	return &Samples{Points: points}, nil
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 32
	}
	return string(b)
}
