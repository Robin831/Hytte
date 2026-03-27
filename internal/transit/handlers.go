package transit

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

const transitStopsPreferenceKey = "transit_stops"

// DeparturesHandler returns real-time departures for the requested stop IDs.
// Query params: stops — comma-separated list of NSR stop IDs.
// When stops is omitted, the user's saved stops (or defaults) are used.
func DeparturesHandler(db *sql.DB, svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		stopsParam := r.URL.Query().Get("stops")

		var stops []FavoriteStop
		if stopsParam != "" {
			// Caller provided explicit stop IDs; construct minimal FavoriteStop entries.
			for _, id := range strings.Split(stopsParam, ",") {
				id = strings.TrimSpace(id)
				if id != "" {
					stops = append(stops, FavoriteStop{ID: id})
				}
			}
		} else {
			// Load from user preferences, falling back to defaults.
			stops = loadFavoriteStops(db, user.ID)
		}

		if len(stops) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"stops": []any{}})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()

		result := make([]StopDepartures, 0, len(stops))
		for _, stop := range stops {
			stopName, departures, err := svc.FetchDepartures(ctx, stop.ID)
			if err != nil {
				// Return a stop entry with no departures rather than failing the whole request.
				result = append(result, StopDepartures{
					StopID:     stop.ID,
					StopName:   stop.Name,
					Departures: []Departure{},
				})
				continue
			}

			// Use the cached name if the API returned none (already cached entry).
			name := stopName
			if name == "" {
				name = stop.Name
			}

			// Filter by configured routes when the stop has a route whitelist.
			filtered := filterDepartures(departures, stop.Routes)

			result = append(result, StopDepartures{
				StopID:     stop.ID,
				StopName:   name,
				Departures: filtered,
			})
		}

		writeJSON(w, http.StatusOK, map[string]any{"stops": result})
	}
}

// SearchHandler proxies stop searches to the Entur Geocoder API.
// Query params: q — search query (required, max 100 chars)
func SearchHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q parameter is required"})
			return
		}
		if len(q) > 100 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q parameter must not exceed 100 characters"})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		results, err := svc.SearchStops(ctx, q)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "stop search failed"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"results": results})
	}
}

// SettingsGetHandler returns the user's saved transit stops.
func SettingsGetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		stops := loadFavoriteStops(db, user.ID)
		writeJSON(w, http.StatusOK, map[string]any{"stops": stops})
	}
}

// SettingsPutHandler saves the user's favorite transit stops.
// Body: {"stops": [{id, name, routes}]}
func SettingsPutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Stops []FavoriteStop `json:"stops"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		data, err := json.Marshal(body.Stops)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to serialize stops"})
			return
		}

		if err := auth.SetPreference(db, user.ID, transitStopsPreferenceKey, string(data)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save stops"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"stops": body.Stops})
	}
}

// loadFavoriteStops retrieves saved stops from preferences, falling back to defaults.
func loadFavoriteStops(db *sql.DB, userID int64) []FavoriteStop {
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		return defaultStops
	}

	raw, ok := prefs[transitStopsPreferenceKey]
	if !ok || raw == "" {
		return defaultStops
	}

	var stops []FavoriteStop
	if err := json.Unmarshal([]byte(raw), &stops); err != nil {
		return defaultStops
	}

	if len(stops) == 0 {
		return defaultStops
	}

	return stops
}

// filterDepartures returns departures whose line code is in the routes whitelist.
// If routes is empty, all departures are returned.
func filterDepartures(departures []Departure, routes []string) []Departure {
	if len(routes) == 0 {
		return departures
	}

	allowed := make(map[string]bool, len(routes))
	for _, r := range routes {
		allowed[r] = true
	}

	filtered := make([]Departure, 0, len(departures))
	for _, d := range departures {
		if allowed[d.Line] {
			filtered = append(filtered, d)
		}
	}
	return filtered
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
