package dashboard

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

// ActivityItem represents a single recent activity entry.
type ActivityItem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Timestamp string `json:"timestamp"`
	Link      string `json:"link,omitempty"`
}

// ActivityHandler returns GET /api/dashboard/activity — recent activity across the app.
func ActivityHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		items, err := recentActivity(db, user.ID)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to load activity"})
			return
		}
		if items == nil {
			items = []ActivityItem{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	}
}

// recentActivity queries the most recent items across multiple tables and
// merges them into a single chronological list, limited to 10 items.
func recentActivity(db *sql.DB, userID int64) ([]ActivityItem, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339)
	var items []ActivityItem

	// Recent workouts.
	rows, err := db.Query(
		`SELECT sport, title, started_at FROM workouts
		 WHERE user_id = ? AND started_at >= ?
		 ORDER BY started_at DESC LIMIT 10`,
		userID, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sport, title, startedAt string
		if err := rows.Scan(&sport, &title, &startedAt); err != nil {
			return nil, err
		}
		label := title
		if label == "" {
			label = sportLabel(sport) + " workout recorded"
		} else {
			label = sportLabel(sport) + ": " + label
		}
		items = append(items, ActivityItem{
			Type:      "workout",
			Title:     label,
			Timestamp: startedAt,
			Link:      "/training",
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Recent lactate tests.
	rows2, err := db.Query(
		`SELECT date, comment FROM lactate_tests
		 WHERE user_id = ? AND date >= ?
		 ORDER BY date DESC LIMIT 10`,
		userID, cutoff[:10], // date column is YYYY-MM-DD
	)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var date, comment string
		if err := rows2.Scan(&date, &comment); err != nil {
			return nil, err
		}
		label := "Lactate test recorded"
		if comment != "" {
			label = "Lactate test: " + comment
		}
		items = append(items, ActivityItem{
			Type:      "lactate",
			Title:     label,
			Timestamp: date + "T00:00:00Z",
			Link:      "/lactate",
		})
	}
	if err := rows2.Err(); err != nil {
		return nil, err
	}

	// Recent notes.
	rows3, err := db.Query(
		`SELECT title, created_at FROM notes
		 WHERE user_id = ? AND created_at >= ?
		 ORDER BY created_at DESC LIMIT 10`,
		userID, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows3.Close()
	for rows3.Next() {
		var title, createdAt string
		if err := rows3.Scan(&title, &createdAt); err != nil {
			return nil, err
		}
		label := "Note created"
		if title != "" {
			label = "Note: " + title
		}
		items = append(items, ActivityItem{
			Type:      "note",
			Title:     label,
			Timestamp: createdAt,
			Link:      "/notes",
		})
	}
	if err := rows3.Err(); err != nil {
		return nil, err
	}

	// Recent short links.
	rows4, err := db.Query(
		`SELECT title, code, strftime('%Y-%m-%dT%H:%M:%SZ', created_at) FROM short_links
		 WHERE user_id = ? AND datetime(created_at) >= datetime(?)
		 ORDER BY created_at DESC LIMIT 10`,
		userID, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows4.Close()
	for rows4.Next() {
		var title, code, createdAt string
		if err := rows4.Scan(&title, &code, &createdAt); err != nil {
			return nil, err
		}
		label := "Short link created: /go/" + code
		if title != "" {
			label = "Link created: " + title
		}
		items = append(items, ActivityItem{
			Type:      "link",
			Title:     label,
			Timestamp: createdAt,
			Link:      "/links",
		})
	}
	if err := rows4.Err(); err != nil {
		return nil, err
	}

	// Sort by timestamp descending and limit to 10.
	sortByTimestamp(items)
	if len(items) > 10 {
		items = items[:10]
	}

	return items, nil
}

func sortByTimestamp(items []ActivityItem) {
	sort.Slice(items, func(i, j int) bool {
		ti, erri := time.Parse(time.RFC3339, items[i].Timestamp)
		tj, errj := time.Parse(time.RFC3339, items[j].Timestamp)
		if erri != nil || errj != nil {
			return items[i].Timestamp > items[j].Timestamp
		}
		return ti.After(tj)
	})
}

func sportLabel(sport string) string {
	switch sport {
	case "running":
		return "Running"
	case "cycling":
		return "Cycling"
	case "swimming":
		return "Swimming"
	case "walking":
		return "Walking"
	case "hiking":
		return "Hiking"
	default:
		if sport != "" {
			return sport
		}
		return "Workout"
	}
}
