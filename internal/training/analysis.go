package training

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
)

// WorkoutAnalysis represents a cached AI analysis of a workout.
type WorkoutAnalysis struct {
	ID           int64  `json:"id"`
	UserID       int64  `json:"user_id"`
	WorkoutID    int64  `json:"workout_id"`
	AnalysisType string `json:"analysis_type"`
	Model        string `json:"model"`
	Prompt       string `json:"prompt,omitempty"`
	ResponseJSON string `json:"response_json,omitempty"`
	Tags         string `json:"tags"`
	Summary      string `json:"summary"`
	Title        string `json:"title"`
	CreatedAt    string `json:"created_at"`
}

// GetAnalysis retrieves a cached analysis for a workout by type.
func GetAnalysis(db *sql.DB, userID, workoutID int64, analysisType string) (*WorkoutAnalysis, error) {
	var a WorkoutAnalysis
	var rawCreatedAt string
	err := db.QueryRow(`
		SELECT id, user_id, workout_id, analysis_type, model, prompt,
		       response_json, tags, summary, title, created_at
		FROM workout_analyses
		WHERE user_id = ? AND workout_id = ? AND analysis_type = ?`,
		userID, workoutID, analysisType).Scan(
		&a.ID, &a.UserID, &a.WorkoutID, &a.AnalysisType, &a.Model,
		&a.Prompt, &a.ResponseJSON, &a.Tags, &a.Summary, &a.Title, &rawCreatedAt,
	)
	if err != nil {
		return nil, err
	}
	// Ensure created_at is RFC3339 regardless of DB storage format.
	a.CreatedAt = normalizeToRFC3339(rawCreatedAt)
	return &a, nil
}

// normalizeToRFC3339 parses a timestamp string and returns RFC3339 format.
func normalizeToRFC3339(s string) string {
	// Try RFC3339 with fractional seconds first.
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.Format(time.RFC3339)
	}
	// Try RFC3339 without fractional seconds.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Format(time.RFC3339)
	}
	// Try SQLite datetime format (YYYY-MM-DD HH:MM:SS).
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	// Fallback: return as-is.
	return s
}

// UpsertAnalysis inserts or replaces an analysis for a workout.
func UpsertAnalysis(db *sql.DB, a *WorkoutAnalysis) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		INSERT INTO workout_analyses (user_id, workout_id, analysis_type, model, prompt, response_json, tags, summary, title, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, workout_id, analysis_type)
		DO UPDATE SET model = excluded.model, prompt = excluded.prompt,
		             response_json = excluded.response_json, tags = excluded.tags,
		             summary = excluded.summary, title = excluded.title,
		             created_at = excluded.created_at`,
		a.UserID, a.WorkoutID, a.AnalysisType, a.Model, a.Prompt,
		a.ResponseJSON, a.Tags, a.Summary, a.Title, now,
	)
	if err == nil {
		a.CreatedAt = now
	}
	return err
}

// DeleteAnalysis removes a cached analysis for a workout.
func DeleteAnalysis(db *sql.DB, userID, workoutID int64, analysisType string) error {
	_, err := db.Exec(`
		DELETE FROM workout_analyses
		WHERE user_id = ? AND workout_id = ? AND analysis_type = ?`,
		userID, workoutID, analysisType)
	return err
}

// AddAITags adds ai:-prefixed tags to a workout, preserving existing tags.
// Verifies that the workout belongs to the given user before writing tags.
func AddAITags(db *sql.DB, workoutID, userID int64, aiTags []string) error {
	// Verify ownership before writing tags.
	var ownerID int64
	err := db.QueryRow(`SELECT user_id FROM workouts WHERE id = ?`, workoutID).Scan(&ownerID)
	if err != nil {
		return fmt.Errorf("workout not found: %w", err)
	}
	if ownerID != userID {
		return fmt.Errorf("workout %d does not belong to user %d", workoutID, userID)
	}

	// Remove old ai: tags first.
	_, err = db.Exec(`DELETE FROM workout_tags WHERE workout_id = ? AND tag GLOB 'ai:*'`, workoutID)
	if err != nil {
		return err
	}
	for _, tag := range aiTags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if !strings.HasPrefix(tag, "ai:") {
			tag = "ai:" + tag
		}
		_, err = db.Exec(`INSERT OR IGNORE INTO workout_tags (workout_id, tag) VALUES (?, ?)`, workoutID, tag)
		if err != nil {
			return err
		}
	}
	return nil
}

// BuildClassificationPrompt constructs the structured prompt for Claude.
func BuildClassificationPrompt(w *Workout) string {
	var sb strings.Builder

	sb.WriteString("Classify this ")
	sb.WriteString(w.Sport)
	sb.WriteString(" workout. Respond with ONLY a JSON object, no markdown formatting.\n\n")

	fmt.Fprintf(&sb, "Sport: %s\n", w.Sport)
	if w.SubSport != "" {
		fmt.Fprintf(&sb, "Sub-sport: %s\n", w.SubSport)
	}
	if w.IsIndoor {
		sb.WriteString("Location: Indoor/Treadmill (no GPS data)\n")
	} else {
		sb.WriteString("Location: Outdoor or unknown (GPS status unknown)\n")
	}
	fmt.Fprintf(&sb, "Duration: %s\n", formatPromptDuration(w.DurationSeconds))
	fmt.Fprintf(&sb, "Distance: %s\n", formatPromptDistance(w.DistanceMeters))
	if w.AvgHeartRate > 0 {
		fmt.Fprintf(&sb, "Avg HR: %d bpm\n", w.AvgHeartRate)
	}

	if len(w.Laps) > 1 {
		sb.WriteString("\nLaps:\n")
		sb.WriteString("| # | Duration | Distance | Avg HR | Pace/km |\n")
		sb.WriteString("|---|----------|----------|--------|---------|\n")
		for _, lap := range w.Laps {
			fmt.Fprintf(&sb, "| %d | %ds | %dm | %d | %s |\n",
				lap.LapNumber,
				int(math.Round(lap.DurationSeconds)),
				int(math.Round(lap.DistanceMeters)),
				lap.AvgHeartRate,
				formatPromptPace(lap.AvgPaceSecPerKm),
			)
		}
	}

	sb.WriteString("\nRespond with a JSON object like: ")
	sb.WriteString(`{"type": "intervals", "tag": "6x6min (r1m)", "summary": "6 intervals of 6 minutes at ~4:44/km with 1 minute recovery jogs", "title": "Threshold Intervals"}`)
	sb.WriteString("\n\nPossible types: easy_run, tempo, threshold, intervals, long_run, recovery, fartlek, race, hill_repeats, warmup_cooldown, other")
	sb.WriteString("\nThe tag should concisely describe the structure (e.g. '6x6min (r1m)', '10k easy', '5k tempo').")
	sb.WriteString("\nThe summary should be a single sentence describing the workout.")
	sb.WriteString("\nThe title should be a short (2-4 word) human-readable workout name like 'Interval Training', 'Long Run', 'Recovery Run', 'Tempo Run', 'Speed Work'. NOT the interval details — that's the tag.")

	return sb.String()
}

func formatPromptDuration(seconds int) string {
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func formatPromptDistance(meters float64) string {
	if meters < 1000 {
		return fmt.Sprintf("%.0f m", meters)
	}
	return fmt.Sprintf("%.2f km", meters/1000)
}

func formatPromptPace(secPerKm float64) string {
	if secPerKm <= 0 {
		return "--:--"
	}
	mins := int(secPerKm) / 60
	secs := int(secPerKm) % 60
	return fmt.Sprintf("%d:%02d", mins, secs)
}
