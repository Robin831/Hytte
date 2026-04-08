package calendar

import "time"

// Event represents a cached Google Calendar event.
type Event struct {
	ID          string `json:"id"`
	CalendarID  string `json:"calendar_id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`
	StartTime   string `json:"start_time"`
	EndTime     string `json:"end_time"`
	AllDay      bool   `json:"all_day"`
	Status      string `json:"status"`
	Color       string `json:"color,omitempty"`
}

// CalendarInfo describes a user's Google Calendar.
type CalendarInfo struct {
	ID              string `json:"id"`
	Summary         string `json:"summary"`
	Description     string `json:"description,omitempty"`
	BackgroundColor string `json:"background_color,omitempty"`
	ForegroundColor string `json:"foreground_color,omitempty"`
	Primary         bool   `json:"primary"`
	Selected        bool   `json:"selected"`
}

// SyncState holds the per-calendar incremental sync token.
type SyncState struct {
	UserID     int64
	CalendarID string
	SyncToken  string
	SyncedAt   time.Time
}
