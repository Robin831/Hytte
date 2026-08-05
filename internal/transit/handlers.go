package transit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Robin831/Hytte/internal/auth"
)

const transitStopsPreferenceKey = "transit_stops"

const (
	maxTransitStops   = 50
	maxStopIDLen      = 100
	maxStopNameLen    = 256
	maxRoutesPerStop  = 100
	maxRouteLen       = 50
	maxWalkMinutes    = 120
	maxSettingsBodySz = 64 << 10 // 64 KB

	// maxConcurrentStopFetches bounds how many Entur requests are in flight at
	// once, so a 50-stop config doesn't fan out into 50 simultaneous calls.
	maxConcurrentStopFetches = 4

	// defaultPerStopTimeout is the deadline each stop fetch gets. It matches the
	// budget the serial implementation shared across every stop, but is now
	// applied per stop so one unresponsive stop can't starve the rest.
	defaultPerStopTimeout = 8 * time.Second
)

// perStopTimeout is the deadline applied to each individual stop fetch.
// It is a variable so tests can shorten it.
var perStopTimeout = defaultPerStopTimeout

// DeparturesHandler returns real-time departures for the requested stop IDs.
// Query params: stops — comma-separated list of NSR stop IDs.
// When stops is omitted, the user's saved stops (or defaults) are used.
func DeparturesHandler(db *sql.DB, svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		stopsParam := r.URL.Query().Get("stops")

		var stops []FavoriteStop
		if stopsParam != "" {
			// Caller provided explicit stop IDs; construct minimal FavoriteStop
			// entries. The walking offset is a property of the stop rather than of
			// the request, so carry it over from the saved favorites when the ID
			// matches one — otherwise a ?stops= request would silently render raw
			// departure times for a stop the user has configured an offset for.
			walkByStopID := make(map[string]int)
			for _, fav := range loadFavoriteStops(db, user.ID) {
				walkByStopID[fav.ID] = fav.WalkMinutes
			}
			for _, id := range strings.Split(stopsParam, ",") {
				id = strings.TrimSpace(id)
				if id != "" {
					stops = append(stops, FavoriteStop{ID: id, WalkMinutes: walkByStopID[id]})
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

		// Fan the per-stop fetches out so total latency tracks the slowest stop
		// instead of the sum of all of them. Results are written into a pre-sized
		// slice by index, so the response order still matches the configured stop
		// order regardless of which fetch finishes first.
		result := make([]StopDepartures, len(stops))

		// A plain errgroup (not WithContext): one stop's failure must not cancel
		// its siblings, so the closures never return an error and instead fold
		// the failure into the stop entry itself.
		var g errgroup.Group
		g.SetLimit(maxConcurrentStopFetches)
		for i, stop := range stops {
			g.Go(func() error {
				result[i] = fetchStopDepartures(r.Context(), svc, stop)
				return nil
			})
		}
		_ = g.Wait()

		writeJSON(w, http.StatusOK, map[string]any{"stops": result})
	}
}

// fetchStopDepartures resolves departures for a single stop under its own
// deadline. It never fails: a stop the upstream can't serve degrades to an
// empty, error-flagged entry so the remaining stops still render.
func fetchStopDepartures(ctx context.Context, svc *Service, stop FavoriteStop) StopDepartures {
	// When route filters are active, fetch more departures so that after
	// filtering there are still enough entries to plan ahead. Without this, all
	// 10 slots could be consumed by other lines, leaving the user with only 1-2
	// departures for their chosen routes.
	count := numberOfDepartures
	if len(stop.Routes) > 0 {
		count = filteredDepartureCount
	}

	// Each stop gets its own timeout, so a single hanging stop burns only its
	// own budget rather than the whole request's.
	stopCtx, cancel := context.WithTimeout(ctx, perStopTimeout)
	defer cancel()

	stopName, departures, err := svc.FetchDepartures(stopCtx, stop.ID, count)
	if err != nil {
		// Return a stop entry with no departures rather than failing the whole
		// request, flagged so the client can say so instead of rendering it as
		// "no departures". When stop.Name is empty (e.g. ad-hoc ID from a query
		// param), fall back to the stop ID so clients always have a displayable
		// label.
		name := stop.Name
		if name == "" {
			name = stop.ID
		}
		return StopDepartures{
			StopID:      stop.ID,
			StopName:    name,
			WalkMinutes: stop.WalkMinutes,
			Departures:  []Departure{},
			Error:       true,
		}
	}

	// Use the cached name if the API returned none (already cached entry).
	name := stopName
	if name == "" {
		name = stop.Name
	}
	if name == "" {
		name = stop.ID
	}

	// Filter by configured routes when the stop has a route whitelist.
	filtered := filterDepartures(departures, stop.Routes)
	if filtered == nil {
		// Never emit `null` — the client iterates this list unconditionally.
		filtered = []Departure{}
	}

	return StopDepartures{
		StopID:      stop.ID,
		StopName:    name,
		WalkMinutes: stop.WalkMinutes,
		Departures:  filtered,
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
// Body: {"stops": [{id, name, routes, walk_minutes}]}
func SettingsPutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodySz)

		var body struct {
			Stops []FavoriteStop `json:"stops"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		if len(body.Stops) > maxTransitStops {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("too many stops (max %d)", maxTransitStops)})
			return
		}
		for _, stop := range body.Stops {
			if len(stop.ID) > maxStopIDLen {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "stop ID too long"})
				return
			}
			if len(stop.Name) > maxStopNameLen {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "stop name too long"})
				return
			}
			if len(stop.Routes) > maxRoutesPerStop {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "too many routes per stop"})
				return
			}
			for _, route := range stop.Routes {
				if len(route) > maxRouteLen {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "route label too long"})
					return
				}
			}
			// Validate before any write so a rejected payload leaves the stored blob untouched.
			if stop.WalkMinutes < 0 || stop.WalkMinutes > maxWalkMinutes {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("walk_minutes must be between 0 and %d", maxWalkMinutes)})
				return
			}
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
