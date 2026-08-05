package auth

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/go-chi/chi/v5"
)

// validCLIPathRe matches safe CLI paths: alphanumeric, slashes, backslashes,
// dots, hyphens, underscores, colons (for Windows drive letters).
var validCLIPathRe = regexp.MustCompile(`^[a-zA-Z0-9._/\\:-]+$`)

// ValidateCLIPath checks that a CLI path contains only safe characters.
// Empty string is valid (means "use default").
func ValidateCLIPath(path string) error {
	if path == "" {
		return nil
	}
	if !validCLIPathRe.MatchString(path) {
		return fmt.Errorf("invalid CLI path: only alphanumeric characters, slashes, dots, hyphens, underscores, and colons are allowed")
	}
	return nil
}

// widgetIDRe matches a dashboard widget id as used by the frontend registry.
var widgetIDRe = regexp.MustCompile(`^[a-z0-9_-]+$`)

const (
	maxDashboardWidgetIDs   = 50
	maxDashboardWidgetIDLen = 64
)

// validateDashboardWidgets checks that the dashboard_widgets preference is a
// JSON object of the form {"order": [...], "hidden": [...]} with a bounded
// number of well-formed widget ids.
func validateDashboardWidgets(raw string) error {
	const shapeErr = `dashboard_widgets must be a JSON object with "order" and "hidden" arrays of widget ids`

	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var layout struct {
		Order  []string `json:"order"`
		Hidden []string `json:"hidden"`
	}
	if err := dec.Decode(&layout); err != nil {
		return fmt.Errorf("%s", shapeErr)
	}

	for _, field := range []struct {
		name string
		ids  []string
	}{{"order", layout.Order}, {"hidden", layout.Hidden}} {
		if len(field.ids) > maxDashboardWidgetIDs {
			return fmt.Errorf("dashboard_widgets %s cannot exceed %d ids", field.name, maxDashboardWidgetIDs)
		}
		for _, id := range field.ids {
			if len(id) > maxDashboardWidgetIDLen {
				return fmt.Errorf("dashboard_widgets %s ids must not exceed %d characters", field.name, maxDashboardWidgetIDLen)
			}
			if !widgetIDRe.MatchString(id) {
				return fmt.Errorf("dashboard_widgets %s ids may only contain lowercase letters, digits, hyphens and underscores", field.name)
			}
		}
	}
	return nil
}

// EventType describes a notification event type that can be filtered.
type EventType struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// AllowedEventTypes is the single source of truth for notification event types.
// Both the validation logic and the GET /api/settings/event-types endpoint use this.
var AllowedEventTypes = []EventType{
	{Key: "push", Label: "Push", Description: "Code pushed to a branch"},
	{Key: "pull_request", Label: "Pull Request", Description: "PR opened, closed, or merged"},
	{Key: "release", Label: "Release", Description: "New release published"},
	{Key: "pr_ready_to_merge", Label: "PR Ready to Merge", Description: "PR passed CI and review, ready to merge"},
	{Key: "pr_created", Label: "PR Created", Description: "Smith created a PR"},
	{Key: "bead_failed", Label: "Bead Failed", Description: "Bead exhausted all retry attempts"},
	{Key: "daily_cost", Label: "Daily Cost", Description: "Daily cost limit reached"},
	{Key: "worker_done", Label: "Worker Done", Description: "Worker completed successfully"},
	{Key: "bead_decomposed", Label: "Bead Decomposed", Description: "Schematic decomposed a parent bead into sub-beads"},
	{Key: "release_published", Label: "Release Published", Description: "New Forge release published"},
}

// allowedEventKeys returns a set derived from AllowedEventTypes for fast lookup.
func allowedEventKeys() map[string]bool {
	m := make(map[string]bool, len(AllowedEventTypes))
	for _, et := range AllowedEventTypes {
		m[et.Key] = true
	}
	return m
}

// EventTypesHandler returns the list of allowed notification event types.
func EventTypesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"event_types": AllowedEventTypes})
	}
}

// PreferencesGetHandler returns all preferences for the authenticated user.
// Claude-related preferences are only visible to admin users.
func PreferencesGetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		prefs, err := GetPreferences(db, user.ID)
		if err != nil {
			log.Printf("Failed to get preferences: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load preferences"})
			return
		}
		if !user.IsAdmin {
			delete(prefs, "claude_enabled")
			delete(prefs, "claude_cli_path")
			delete(prefs, "claude_model")
		} else if raw, ok := prefs["claude_cli_path"]; ok && raw != "" {
			decrypted, err := encryption.DecryptField(raw)
			if err != nil {
				log.Printf("Warning: failed to decrypt claude_cli_path, omitting from response: %v", err)
				delete(prefs, "claude_cli_path")
			} else {
				prefs["claude_cli_path"] = decrypted
			}
		}
		// Decrypt stride_custom_prompt (user-generated text, encrypted at rest).
		if raw, ok := prefs["stride_custom_prompt"]; ok && raw != "" {
			decrypted, err := encryption.DecryptField(raw)
			if err != nil {
				log.Printf("Warning: failed to decrypt stride_custom_prompt, omitting from response: %v", err)
				delete(prefs, "stride_custom_prompt")
			} else {
				prefs["stride_custom_prompt"] = decrypted
			}
		}
		// Mask Wordfeud credentials so the UI knows they exist without exposing them.
		for _, key := range []string{"wordfeud_session_token", "wordfeud_email", "wordfeud_password"} {
			if raw, ok := prefs[key]; ok && raw != "" {
				prefs[key] = "configured"
			}
		}
		// Ensure partner_income is always present with its default so API consumers
		// can rely on the key existing even before the user has set it.
		if _, ok := prefs["partner_income"]; !ok {
			prefs["partner_income"] = "0"
		}
		writeJSON(w, http.StatusOK, map[string]any{"preferences": prefs})
	}
}

// PreferencesPutHandler updates preferences for the authenticated user.
// Expects JSON body: {"preferences": {"key": "value", ...}}
func PreferencesPutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())

		var rawBody struct {
			Preferences map[string]json.RawMessage `json:"preferences"`
		}
		if err := json.NewDecoder(r.Body).Decode(&rawBody); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		// Convert each raw JSON value to a string. For most preferences we require
		// the JSON value to be a string; for a small set of JSON-typed preferences
		// we accept any JSON value and store its compact JSON representation.
		body := struct{ Preferences map[string]string }{
			Preferences: make(map[string]string, len(rawBody.Preferences)),
		}

		// Preference keys whose values are intentionally stored as arbitrary JSON.
		jsonTypedPrefs := map[string]bool{
			"quick_links":                true,
			"notification_filter_events": true,
			"dashboard_widgets":          true,
		}

		for k, raw := range rawBody.Preferences {
			// JSON-typed preferences: accept any JSON value and store its compact JSON representation.
			if jsonTypedPrefs[k] {
				var buf bytes.Buffer
				if err := json.Compact(&buf, raw); err != nil {
					body.Preferences[k] = string(raw)
				} else {
					body.Preferences[k] = buf.String()
				}
				continue
			}

			// For all other preferences, require a JSON string.
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": fmt.Sprintf("preference %q must be a JSON string", k),
				})
				return
			}
			body.Preferences[k] = s
		}

		// Only allow known preference keys.
		allowed := map[string]bool{
			"theme":                           true,
			"home_location":                   true,
			"weather_location":                true,
			"skywatch_location":               true,
			"recent_locations":                true,
			"notifications_enabled":           true,
			"notifications_degraded":          true,
			"quiet_hours_enabled":             true,
			"quiet_hours_start":               true,
			"quiet_hours_end":                 true,
			"quiet_hours_timezone":            true,
			"notification_filter_sources":     true,
			"notification_filter_events":      true,
			"max_hr":                          true,
			"threshold_hr":                    true,
			"threshold_pace":                  true,
			"easy_pace_min":                   true,
			"easy_pace_max":                   true,
			"resting_hr":                      true,
			"quick_links":                     true,
			"claude_enabled":                  true,
			"claude_cli_path":                 true,
			"claude_model":                    true,
			"ai_trend_weeks":                  true,
			"ai_auto_analyze":                 true,
			"goal_race_name":                  true,
			"goal_race_date":                  true,
			"goal_race_distance":              true,
			"goal_race_target_time":           true,
			"kids_stars_leaderboard_visible":  true,
			"kids_stars_parent_participates":  true,
			"work_hours_standard_day":         true,
			"work_hours_rounding":             true,
			"work_hours_lunch_minutes":        true,
			"work_hours_flex_reset_date":      true,
			"work_hours_vacation_allowance":   true,
			"zone_boundaries":                 true,
			"income_split_percentage":         true,
			"partner_income":                  true,
			"income_day":                      true,
			"partner_income_day":              true,
			"stride_custom_prompt":            true,
			"calendar_visible_ids":            true,
			"regnemester_muted":               true,
			"pokemon_scan_daily_cap":          true,
			"pokemon_scan_push_enabled":       true,
			"pokemon_scan_auto_discard_hours": true,
			"dashboard_widgets":               true,
		}

		// Integer range keys: HR/pace, work hours, budget preferences, and other numeric settings.
		intRangeKeys := map[string]struct{ min, max int }{
			"max_hr":                          {100, 230},
			"threshold_hr":                    {100, 220},
			"resting_hr":                      {30, 100},
			"threshold_pace":                  {120, 1200}, // 2:00-20:00 per km
			"easy_pace_min":                   {120, 1200},
			"easy_pace_max":                   {120, 1200},
			"ai_trend_weeks":                  {1, 52},
			"work_hours_standard_day":         {60, 960},     // 1h–16h in minutes
			"work_hours_lunch_minutes":        {0, 120},      // 0–2h
			"work_hours_vacation_allowance":   {1, 100},      // 1–100 days/year
			"income_split_percentage":         {0, 100},      // 0–100 %
			"partner_income":                  {0, 10000000}, // monthly salary in NOK; must match budget.maxPartnerIncome
			"income_day":                      {1, 31},       // day of month; must match budget.defaultIncomeDay
			"partner_income_day":              {1, 31},
			"pokemon_scan_daily_cap":          {1, 100000}, // override for ScanDailyCap (600); upper bound keeps a typo from disabling the cap
			"pokemon_scan_auto_discard_hours": {0, 168},    // 0 disables auto-discard for this user; cap at one week
		}

		allowedEvents := allowedEventKeys()

		claudeKeys := map[string]bool{
			"claude_enabled":  true,
			"claude_cli_path": true,
			"claude_model":    true,
		}

		// Build the set of keys to process (skip unknown keys).
		toWrite := make(map[string]string, len(body.Preferences))
		for k, v := range body.Preferences {
			if allowed[k] {
				toWrite[k] = v
			}
		}

		// Pre-validate all keys before writing any, so the request is atomic:
		// either all accepted preferences are persisted or none are.
		for k, v := range toWrite {
			if claudeKeys[k] && !user.IsAdmin {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "Claude AI features are restricted to admin users"})
				return
			}
			// Validate integer range keys (empty string means "clear the value").
			if bounds, ok := intRangeKeys[k]; ok && v != "" {
				n, err := strconv.Atoi(v)
				if err != nil || n < bounds.min || n > bounds.max {
					writeJSON(w, http.StatusBadRequest, map[string]string{
						"error": fmt.Sprintf("%s must be an integer between %d and %d", k, bounds.min, bounds.max),
					})
					return
				}
			}
			// Validate quick_links: must be a JSON array of {title, url} with safe URLs.
			if k == "quick_links" {
				var links []struct {
					Title string `json:"title"`
					URL   string `json:"url"`
				}
				if err := json.Unmarshal([]byte(v), &links); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quick_links must be a JSON array of {title, url} objects"})
					return
				}
				if len(links) > 50 {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quick_links cannot exceed 50 items"})
					return
				}
				for _, link := range links {
					if strings.TrimSpace(link.Title) == "" || strings.TrimSpace(link.URL) == "" {
						writeJSON(w, http.StatusBadRequest, map[string]string{"error": "each quick link must have a non-empty title and url"})
						return
					}
					if len(link.Title) > 200 {
						writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quick link title must not exceed 200 characters"})
						return
					}
					if len(link.URL) > 2048 {
						writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quick link URL must not exceed 2048 characters"})
						return
					}
					parsed, err := url.Parse(link.URL)
					if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
						writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quick link URLs must use http or https with a valid host"})
						return
					}
				}
			}
			// Validate skywatch_location: a JSON object {name, lat, lon} with sane coordinates.
			// An empty name is allowed — it marks coordinates from browser geolocation.
			if k == "skywatch_location" && v != "" {
				var loc struct {
					Name string   `json:"name"`
					Lat  *float64 `json:"lat"`
					Lon  *float64 `json:"lon"`
				}
				if err := json.Unmarshal([]byte(v), &loc); err != nil || loc.Lat == nil || loc.Lon == nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "skywatch_location must be a JSON object with name, lat and lon"})
					return
				}
				if *loc.Lat < -90 || *loc.Lat > 90 || *loc.Lon < -180 || *loc.Lon > 180 {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "skywatch_location coordinates are out of range"})
					return
				}
				if len(loc.Name) > 100 {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "skywatch_location name must not exceed 100 characters"})
					return
				}
			}
			// Validate CLI path to prevent command injection.
			if k == "claude_cli_path" {
				if err := ValidateCLIPath(v); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
			}
			// Validate event keys inside notification_filter_events JSON.
			if k == "notification_filter_events" {
				var events map[string]bool
				if err := json.Unmarshal([]byte(v), &events); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "notification_filter_events must be a JSON object mapping event keys to booleans"})
					return
				}
				for ek := range events {
					if !allowedEvents[ek] {
						writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown event type: " + ek})
						return
					}
				}
			}
			// Validate dashboard_widgets: a JSON object {order, hidden} of widget id
			// arrays. Ids are not checked against a widget registry — the frontend
			// ignores ids it does not know — but the shape and size are bounded so a
			// malformed client cannot store junk in the preference.
			if k == "dashboard_widgets" && v != "" {
				if err := validateDashboardWidgets(v); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
			}
			// Validate work_hours_rounding: must be 15, 30, or 60 minutes.
			if k == "work_hours_rounding" && v != "" {
				if v != "15" && v != "30" && v != "60" {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "work_hours_rounding must be 15, 30, or 60"})
					return
				}
			}
			// Validate work_hours_flex_reset_date: must be YYYY-MM-DD or empty.
			if k == "work_hours_flex_reset_date" && v != "" {
				if _, err := time.Parse("2006-01-02", v); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "work_hours_flex_reset_date must be in YYYY-MM-DD format"})
					return
				}
			}
			// Validate zone_boundaries: must be a JSON array of 5 zone objects with zone, min_bpm, max_bpm.
			// Zones 1–5 must each appear exactly once, and boundaries must be monotonically increasing.
			// Accepts both a raw JSON array ([{...}]) and a JSON-string-wrapped array ("[{...}]") for
			// backwards compatibility with older clients.
			if k == "zone_boundaries" && v != "" {
				// Normalise: if stored as a JSON string (e.g. `"[{...}]"`), unwrap it first.
				rawArray := v
				if len(v) > 0 && v[0] == '"' {
					var s string
					if err := json.Unmarshal([]byte(v), &s); err == nil {
						rawArray = s
					}
				}
				var zones []struct {
					Zone   int `json:"zone"`
					MinBPM int `json:"min_bpm"`
					MaxBPM int `json:"max_bpm"`
				}
				if err := json.Unmarshal([]byte(rawArray), &zones); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "zone_boundaries must be a JSON array of {zone, min_bpm, max_bpm} objects"})
					return
				}
				if len(zones) != 5 {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "zone_boundaries must contain exactly 5 zones"})
					return
				}
				seen := make(map[int]bool, 5)
				for _, z := range zones {
					if z.Zone < 1 || z.Zone > 5 {
						writeJSON(w, http.StatusBadRequest, map[string]string{"error": "zone_boundaries: zone must be between 1 and 5"})
						return
					}
					if seen[z.Zone] {
						writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("zone_boundaries: zone %d appears more than once", z.Zone)})
						return
					}
					seen[z.Zone] = true
					if z.MinBPM < 0 || z.MaxBPM < 0 || z.MaxBPM <= z.MinBPM {
						writeJSON(w, http.StatusBadRequest, map[string]string{"error": "zone_boundaries: max_bpm must be greater than min_bpm"})
						return
					}
					if z.MaxBPM > 300 {
						writeJSON(w, http.StatusBadRequest, map[string]string{"error": "zone_boundaries: max_bpm must not exceed 300"})
						return
					}
				}
				// Verify zones 1–5 are all present.
				for zn := 1; zn <= 5; zn++ {
					if !seen[zn] {
						writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("zone_boundaries: zone %d is missing", zn)})
						return
					}
				}
				// Sort by zone number and verify monotonically increasing boundaries.
				byZone := make([]struct{ MinBPM, MaxBPM int }, 5)
				for _, z := range zones {
					byZone[z.Zone-1] = struct{ MinBPM, MaxBPM int }{z.MinBPM, z.MaxBPM}
				}
				for i := 1; i < 5; i++ {
					if byZone[i].MinBPM < byZone[i-1].MaxBPM {
						writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("zone_boundaries: zone %d min_bpm must be >= zone %d max_bpm", i+1, i)})
						return
					}
				}
			}
		}

		// Encrypt Wordfeud credentials before persisting.
		for _, key := range []string{"wordfeud_email", "wordfeud_password", "wordfeud_session_token"} {
			if val, ok := toWrite[key]; ok && val != "" {
				enc, err := encryption.EncryptField(val)
				if err != nil {
					log.Printf("Failed to encrypt %s: %v", key, err)
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save preferences"})
					return
				}
				toWrite[key] = enc
			}
		}

		// Encrypt claude_cli_path and stride_custom_prompt before persisting.
		for _, key := range []string{"claude_cli_path", "stride_custom_prompt"} {
			if val, ok := toWrite[key]; ok && val != "" {
				enc, err := encryption.EncryptField(val)
				if err != nil {
					log.Printf("Failed to encrypt %s: %v", key, err)
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save preferences"})
					return
				}
				toWrite[key] = enc
			}
		}

		// All keys validated — now persist them atomically in a single
		// transaction so a batched write is all-or-nothing.
		if err := SetPreferences(db, user.ID, toWrite); err != nil {
			log.Printf("Failed to set preferences: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save preferences"})
			return
		}

		prefs, err := GetPreferences(db, user.ID)
		if err != nil {
			log.Printf("Failed to get preferences after update: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load preferences"})
			return
		}
		// Mirror GET handler: non-admins must not see Claude-related preferences
		// in the response. Admins get the decrypted value.
		if !user.IsAdmin {
			delete(prefs, "claude_enabled")
			delete(prefs, "claude_cli_path")
			delete(prefs, "claude_model")
		} else if raw, ok := prefs["claude_cli_path"]; ok && raw != "" {
			decrypted, decErr := encryption.DecryptField(raw)
			if decErr != nil {
				log.Printf("Warning: failed to decrypt claude_cli_path in PUT response: %v", decErr)
				delete(prefs, "claude_cli_path")
			} else {
				prefs["claude_cli_path"] = decrypted
			}
		}
		// Decrypt stride_custom_prompt in PUT response (mirrors GET handler).
		if raw, ok := prefs["stride_custom_prompt"]; ok && raw != "" {
			decrypted, decErr := encryption.DecryptField(raw)
			if decErr != nil {
				log.Printf("Warning: failed to decrypt stride_custom_prompt in PUT response: %v", decErr)
				delete(prefs, "stride_custom_prompt")
			} else {
				prefs["stride_custom_prompt"] = decrypted
			}
		}
		// Mask Wordfeud credentials in PUT response (mirrors GET handler).
		for _, key := range []string{"wordfeud_session_token", "wordfeud_email", "wordfeud_password"} {
			if raw, ok := prefs[key]; ok && raw != "" {
				prefs[key] = "configured"
			}
		}
		// Ensure partner_income is always present with its default (mirrors GET handler).
		if _, ok := prefs["partner_income"]; !ok {
			prefs["partner_income"] = "0"
		}
		writeJSON(w, http.StatusOK, map[string]any{"preferences": prefs})
	}
}

// SessionsListHandler returns the active sessions for the authenticated user.
func SessionsListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())

		// Get the current session token hash to mark which one is "current".
		var currentTokenHash string
		if cookie, err := r.Cookie("session"); err == nil {
			currentTokenHash = hashToken(cookie.Value)
		}

		rows, err := db.Query(
			"SELECT token, created_at, expires_at, user_agent, last_seen_at FROM sessions WHERE user_id = ? AND expires_at > ? ORDER BY created_at DESC",
			user.ID, time.Now(),
		)
		if err != nil {
			log.Printf("Failed to list sessions: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list sessions"})
			return
		}
		defer rows.Close()

		type sessionInfo struct {
			ID          string  `json:"id"`
			CreatedAt   string  `json:"created_at"`
			ExpiresAt   string  `json:"expires_at"`
			DeviceLabel string  `json:"device_label"`
			LastSeenAt  *string `json:"last_seen_at"`
			Current     bool    `json:"current"`
		}

		var sessions []sessionInfo
		for rows.Next() {
			var token, userAgent string
			var createdAt, expiresAt time.Time
			var lastSeenAt sql.NullTime
			if err := rows.Scan(&token, &createdAt, &expiresAt, &userAgent, &lastSeenAt); err != nil {
				log.Printf("Failed to scan session: %v", err)
				continue
			}
			// Use a short prefix as ID so we don't expose the full token.
			// Guard against unexpectedly short tokens.
			displayID := token
			if len(displayID) > 8 {
				displayID = displayID[:8]
			}
			var lastSeen *string
			if lastSeenAt.Valid {
				formatted := lastSeenAt.Time.Format(time.RFC3339)
				lastSeen = &formatted
			}
			sessions = append(sessions, sessionInfo{
				ID:          displayID,
				CreatedAt:   createdAt.Format(time.RFC3339),
				ExpiresAt:   expiresAt.Format(time.RFC3339),
				DeviceLabel: DeviceLabel(decryptSessionUserAgent(userAgent)),
				LastSeenAt:  lastSeen,
				Current:     token == currentTokenHash,
			})
		}
		if sessions == nil {
			sessions = []sessionInfo{}
		}

		writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
	}
}

// decryptSessionUserAgent decrypts a stored session user agent. Sessions created
// before user agents were captured hold an empty string, and a decrypt failure
// means the value is unusable — both return "" so the caller falls back to a
// generic device label.
func decryptSessionUserAgent(stored string) string {
	if stored == "" {
		return ""
	}
	ua, err := encryption.DecryptField(stored)
	if err != nil {
		log.Printf("Warning: failed to decrypt session user agent: %v", err)
		return ""
	}
	return ua
}

// sessionIDRe matches the token prefix used as a session ID in the API. Session
// tokens are hex, so anything else can never match a row.
var sessionIDRe = regexp.MustCompile(`^[0-9a-f]{4,64}$`)

// SessionRevokeHandler deletes a single session belonging to the authenticated
// user, identified by the token prefix returned from SessionsListHandler.
func SessionRevokeHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())

		id := chi.URLParam(r, "id")
		if !sessionIDRe.MatchString(id) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}

		var currentTokenHash string
		if cookie, err := r.Cookie("session"); err == nil {
			currentTokenHash = hashToken(cookie.Value)
		}

		// Resolve the prefix against this user's sessions only, so one user can
		// never revoke (or probe for) another user's session.
		rows, err := db.Query(
			"SELECT token FROM sessions WHERE user_id = ? AND token LIKE ? || '%'",
			user.ID, id,
		)
		if err != nil {
			log.Printf("Failed to look up session for revoke: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to revoke session"})
			return
		}
		defer rows.Close()

		var matches []string
		for rows.Next() {
			var token string
			if err := rows.Scan(&token); err != nil {
				log.Printf("Failed to scan session for revoke: %v", err)
				continue
			}
			matches = append(matches, token)
		}
		if err := rows.Err(); err != nil {
			log.Printf("Failed to iterate sessions for revoke: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to revoke session"})
			return
		}

		// An unknown or ambiguous prefix is indistinguishable from "no such
		// session" as far as the caller is concerned.
		if len(matches) != 1 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		if matches[0] == currentTokenHash {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot revoke the current session; sign out instead"})
			return
		}

		if _, err := db.Exec(
			"DELETE FROM sessions WHERE token = ? AND user_id = ?",
			matches[0], user.ID,
		); err != nil {
			log.Printf("Failed to revoke session: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to revoke session"})
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// SignOutEverywhereHandler deletes all sessions for the authenticated user
// except the current one.
func SignOutEverywhereHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())

		var currentTokenHash string
		if cookie, err := r.Cookie("session"); err == nil {
			currentTokenHash = hashToken(cookie.Value)
		}

		_, err := db.Exec(
			"DELETE FROM sessions WHERE user_id = ? AND token != ?",
			user.ID, currentTokenHash,
		)
		if err != nil {
			log.Printf("Failed to sign out everywhere: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to sign out other sessions"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// DeleteAccountHandler removes the user's account and all related data.
func DeleteAccountHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())

		// Delete user — CASCADE will remove sessions and preferences.
		_, err := db.Exec("DELETE FROM users WHERE id = ?", user.ID)
		if err != nil {
			log.Printf("Failed to delete account: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete account"})
			return
		}

		// Clear the session cookie.
		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   isSecure(),
		})

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
