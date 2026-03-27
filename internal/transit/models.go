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
type StopDepartures struct {
	StopID     string      `json:"stop_id"`
	StopName   string      `json:"stop_name"`
	Departures []Departure `json:"departures"`
}

// FavoriteStop is a user-configured stop with optional route filtering.
// When Routes is empty, all departures from the stop are shown.
type FavoriteStop struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Routes []string `json:"routes"`
}

// defaultStops are the default favorite stops shown before the user configures their own.
// Bjørndalsbakken and Olav Kyrres gate, filtered to bus 3 and 3E westbound.
var defaultStops = []FavoriteStop{
	{ID: "NSR:StopPlace:42175", Name: "Bjørndalsbakken", Routes: []string{"3", "3E"}},
	{ID: "NSR:StopPlace:44179", Name: "Olav Kyrres gate", Routes: []string{"3", "3E"}},
}
