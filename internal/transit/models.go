package transit

import "time"

// Departure represents a single departure from a transit stop.
type Departure struct {
	Line          string    `json:"line"`
	Destination   string    `json:"destination"`
	DepartureTime time.Time `json:"departure_time"`
	IsRealtime    bool      `json:"is_realtime"`
	Platform      string    `json:"platform,omitempty"`
	DelayMinutes  int       `json:"delay_minutes"`
}

// StopDepartures groups departures for a single stop.
// WalkMinutes mirrors the stop's configured walking offset so the departures
// payload alone is enough to render "time to leave" — the client only loads
// /api/transit/settings when the settings panel is open.
type StopDepartures struct {
	StopID      string      `json:"stop_id"`
	StopName    string      `json:"stop_name"`
	WalkMinutes int         `json:"walk_minutes"`
	Departures  []Departure `json:"departures"`
}

// FavoriteStop is a user-configured stop with optional route filtering.
// When Routes is empty, all departures from the stop are shown.
// WalkMinutes is the time it takes the user to walk to the stop; it is
// subtracted from each departure so the UI can show time-to-leave. Stops saved
// before this field existed decode as 0, which keeps the raw departure time.
type FavoriteStop struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Routes      []string `json:"routes"`
	WalkMinutes int      `json:"walk_minutes"`
}

// defaultStops is empty — users add their own stops via the search function.
var defaultStops []FavoriteStop
