package dashboard

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
)

// ActivityItem represents a single recent activity entry. The frontend is
// responsible for assembling localized display text from these structured
// fields via react-i18next.
type ActivityItem struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Link      string `json:"link,omitempty"`
	Sport     string `json:"sport,omitempty"`
	Title     string `json:"title,omitempty"`
	Comment   string `json:"comment,omitempty"`
	Code      string `json:"code,omitempty"`
}

// ActivityHandler returns GET /api/dashboard/activity — recent activity across the app.
func ActivityHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		// The endpoint is registered outside every RequireFeature group, so
		// resolve the caller's feature set here and only query the sources
		// they can access. Prefer the map cached by WithFeatures; fall back to
		// a DB lookup (which returns all-true for admins), mirroring
		// auth.RequireFeature.
		features := auth.FeaturesFromContext(r.Context())
		if features == nil {
			var err error
			features, err = auth.GetUserFeatures(db, user.ID, user.IsAdmin)
			if err != nil {
				writeActivityError(w)
				return
			}
		}

		items, err := recentActivity(db, user.ID, features)
		if err != nil {
			writeActivityError(w)
			return
		}
		if items == nil {
			items = []ActivityItem{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	}
}

func writeActivityError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to load activity"})
}

// activitySources maps each activity query helper to the feature key that
// gates it. Sources whose feature is disabled for the user are never queried.
var activitySources = []struct {
	feature string
	query   func(*sql.DB, int64, string) ([]ActivityItem, error)
}{
	{"training", queryWorkouts},
	{"lactate", queryLactate},
	{"notes", queryNotes},
	{"links", queryLinks},
}

// recentActivity queries the most recent items across multiple tables and
// merges them into a single chronological list, limited to 10 items. Only
// sources whose feature is enabled in features are queried.
//
// Each source is queried via its own helper so the sql.Rows handle is scoped
// to that helper (defer rows.Close runs on helper return), guaranteeing no
// rows are left open if a later source fails.
func recentActivity(db *sql.DB, userID int64, features map[string]bool) ([]ActivityItem, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339)

	var items []ActivityItem
	for _, src := range activitySources {
		if !features[src.feature] {
			continue
		}
		got, err := src.query(db, userID, cutoff)
		if err != nil {
			return nil, err
		}
		items = append(items, got...)
	}

	sortByTimestamp(items)
	if len(items) > 10 {
		items = items[:10]
	}
	return items, nil
}

// queryWorkouts returns up to 10 recent workout activity items for the user.
func queryWorkouts(db *sql.DB, userID int64, cutoff string) ([]ActivityItem, error) {
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

	var items []ActivityItem
	for rows.Next() {
		var sport, title, startedAt string
		if err := rows.Scan(&sport, &title, &startedAt); err != nil {
			return nil, err
		}
		title = encryption.DecryptLenient(title)
		items = append(items, ActivityItem{
			Type:      "workout",
			Sport:     sport,
			Title:     title,
			Timestamp: startedAt,
			Link:      "/training",
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// queryLactate returns up to 10 recent lactate-test activity items for the user.
// The lactate_tests.date column stores YYYY-MM-DD, so the RFC3339 cutoff is
// truncated to its date prefix.
func queryLactate(db *sql.DB, userID int64, cutoff string) ([]ActivityItem, error) {
	rows, err := db.Query(
		`SELECT date, comment FROM lactate_tests
		 WHERE user_id = ? AND date >= ?
		 ORDER BY date DESC LIMIT 10`,
		userID, cutoff[:10],
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ActivityItem
	for rows.Next() {
		var date, comment string
		if err := rows.Scan(&date, &comment); err != nil {
			return nil, err
		}
		comment = encryption.DecryptLenient(comment)
		items = append(items, ActivityItem{
			Type:      "lactate",
			Comment:   comment,
			Timestamp: date + "T00:00:00Z",
			Link:      "/lactate",
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// queryNotes returns up to 10 recent note activity items for the user.
func queryNotes(db *sql.DB, userID int64, cutoff string) ([]ActivityItem, error) {
	rows, err := db.Query(
		`SELECT title, created_at FROM notes
		 WHERE user_id = ? AND created_at >= ?
		 ORDER BY created_at DESC LIMIT 10`,
		userID, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ActivityItem
	for rows.Next() {
		var title, createdAt string
		if err := rows.Scan(&title, &createdAt); err != nil {
			return nil, err
		}
		title = encryption.DecryptLenient(title)
		items = append(items, ActivityItem{
			Type:      "note",
			Title:     title,
			Timestamp: createdAt,
			Link:      "/notes",
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// queryLinks returns up to 10 recent short-link activity items for the user.
func queryLinks(db *sql.DB, userID int64, cutoff string) ([]ActivityItem, error) {
	rows, err := db.Query(
		`SELECT title, code, strftime('%Y-%m-%dT%H:%M:%SZ', created_at) FROM short_links
		 WHERE user_id = ? AND datetime(created_at) >= datetime(?)
		 ORDER BY created_at DESC LIMIT 10`,
		userID, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ActivityItem
	for rows.Next() {
		var title, code, createdAt string
		if err := rows.Scan(&title, &code, &createdAt); err != nil {
			return nil, err
		}
		items = append(items, ActivityItem{
			Type:      "link",
			Code:      code,
			Title:     title,
			Timestamp: createdAt,
			Link:      "/links",
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
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
