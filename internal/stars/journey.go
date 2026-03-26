package stars

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/family"
	"github.com/Robin831/Hytte/internal/push"
)

// Waypoint is a named location in a story journey.
type Waypoint struct {
	Name        string  `json:"name"`
	DistanceKm  float64 `json:"distance_km"`
	Description string  `json:"description"`
	Emoji       string  `json:"emoji"`
}

// Theme is a named story journey with a sequence of waypoints.
type Theme struct {
	Key             string     `json:"key"`
	Name            string     `json:"name"`
	Emoji           string     `json:"emoji"`
	TotalDistanceKm float64    `json:"total_distance_km"`
	Waypoints       []Waypoint `json:"waypoints"`
}

// themes contains the three built-in story journey themes.
var themes = map[string]Theme{
	"middle_earth": {
		Key:             "middle_earth",
		Name:            "Middle-earth",
		Emoji:           "💍",
		TotalDistanceKm: 1350,
		Waypoints: []Waypoint{
			{Name: "The Shire", DistanceKm: 0, Description: "Your adventure begins in the peaceful green hills of the Shire.", Emoji: "🏡"},
			{Name: "Bree", DistanceKm: 100, Description: "You've reached the bustling crossroads town of Bree. An innkeeper offers you a pint!", Emoji: "🏰"},
			{Name: "Weathertop", DistanceKm: 250, Description: "You've climbed to the ancient watchtower of Weathertop. Watch out for shadows in the night!", Emoji: "⚡"},
			{Name: "Rivendell", DistanceKm: 400, Description: "Welcome to the elven haven of Rivendell! Rest among the waterfalls and wise elves.", Emoji: "🌿"},
			{Name: "Moria", DistanceKm: 600, Description: "You've passed through the dark mines of Moria. You shall not pass... or shall you?", Emoji: "⛏️"},
			{Name: "Lothlórien", DistanceKm: 750, Description: "The golden forest of Lothlórien glows around you. The Lady Galadriel watches your journey.", Emoji: "✨"},
			{Name: "Rauros Falls", DistanceKm: 900, Description: "You hear the roar of the great Rauros Falls. The fellowship is tested here!", Emoji: "💧"},
			{Name: "Minas Tirith", DistanceKm: 1100, Description: "The White City stands before you! Climb to the top for a view of all Middle-earth.", Emoji: "🏯"},
			{Name: "Mount Doom", DistanceKm: 1350, Description: "You've done it! The fires of Mount Doom roar as you complete your epic quest!", Emoji: "🌋"},
		},
	},
	"space": {
		Key:             "space",
		Name:            "Solar System Explorer",
		Emoji:           "🚀",
		TotalDistanceKm: 1350,
		Waypoints: []Waypoint{
			{Name: "Earth", DistanceKm: 0, Description: "Blastoff! Your space adventure begins on our beautiful blue planet.", Emoji: "🌍"},
			{Name: "The Moon", DistanceKm: 100, Description: "You've reached the Moon! One small step for a runner, one giant leap for fitness!", Emoji: "🌙"},
			{Name: "Asteroid Belt", DistanceKm: 300, Description: "Dodge those space rocks! You've navigated through the asteroid belt like a pro.", Emoji: "☄️"},
			{Name: "Mars", DistanceKm: 500, Description: "You've landed on the Red Planet! Plant your flag and look for friendly Martians.", Emoji: "🔴"},
			{Name: "Jupiter", DistanceKm: 750, Description: "The Great Red Spot swirls below as you orbit the biggest planet in the solar system!", Emoji: "🪐"},
			{Name: "Saturn", DistanceKm: 1000, Description: "Incredible! You're surfing Saturn's rings. What a view from up here!", Emoji: "💫"},
			{Name: "Uranus", DistanceKm: 1200, Description: "The ice giant Uranus spins sideways — just like you when you run really fast!", Emoji: "🧊"},
			{Name: "Neptune & Beyond", DistanceKm: 1350, Description: "You've reached the edge of our solar system! The cosmos stretches infinitely ahead.", Emoji: "🌌"},
		},
	},
	"pirate": {
		Key:             "pirate",
		Name:            "Pirate Adventure",
		Emoji:           "🏴‍☠️",
		TotalDistanceKm: 1350,
		Waypoints: []Waypoint{
			{Name: "Port Royal", DistanceKm: 0, Description: "Ahoy! Your pirate adventure begins at the lively docks of Port Royal. All hands on deck!", Emoji: "⚓"},
			{Name: "Tortuga", DistanceKm: 100, Description: "You've sailed to Tortuga, the wildest pirate port in the Caribbean! Try the rum (or apple juice).", Emoji: "🍺"},
			{Name: "Nassau", DistanceKm: 250, Description: "The pirate republic of Nassau flies the skull and crossbones! You're a real buccaneer now.", Emoji: "🏴‍☠️"},
			{Name: "The Bermuda Triangle", DistanceKm: 450, Description: "You've survived the mysterious Bermuda Triangle! Strange things happen here... or do they?", Emoji: "🌀"},
			{Name: "Davy Jones' Locker", DistanceKm: 650, Description: "You dove to the depths and returned from Davy Jones' Locker with incredible treasures!", Emoji: "💀"},
			{Name: "Isla de Muerta", DistanceKm: 850, Description: "The hidden Isla de Muerta rises from the fog! A chest of cursed Aztec gold awaits.", Emoji: "💰"},
			{Name: "The Kraken's Lair", DistanceKm: 1100, Description: "Release the Kraken! You've bravely sailed past the legendary sea monster's territory.", Emoji: "🐙"},
			{Name: "World's End", DistanceKm: 1350, Description: "You've sailed to the very edge of the world! The greatest pirate adventure ever completed!", Emoji: "🌊"},
		},
	},
}

// validThemes contains the allowed theme keys.
var validThemes = map[string]bool{
	"middle_earth": true,
	"space":        true,
	"pirate":       true,
}

// Journey is the persistent state of a user's story journey.
type Journey struct {
	ID               int64   `json:"id"`
	UserID           int64   `json:"user_id"`
	Theme            string  `json:"theme"`
	TotalDistanceM   float64 `json:"total_distance_m"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

// JourneyResponse is the full journey state returned by the API.
type JourneyResponse struct {
	Theme               string     `json:"theme"`
	ThemeName           string     `json:"theme_name"`
	ThemeEmoji          string     `json:"theme_emoji"`
	TotalDistanceM      float64    `json:"total_distance_m"`
	TotalJourneyDistKm  float64    `json:"total_journey_distance_km"`
	CurrentWaypoint     Waypoint   `json:"current_waypoint"`
	NextWaypoint        *Waypoint  `json:"next_waypoint"`
	LegDescription      string     `json:"leg_description"`
	ProgressInLegPct    float64    `json:"progress_in_leg_percent"`
	DistanceToNextKm    float64    `json:"distance_to_next_km"`
	Waypoints           []WaypointStatus `json:"waypoints"`
	AvailableThemes     []Theme    `json:"available_themes"`
}

// WaypointStatus extends Waypoint with a crossed indicator.
type WaypointStatus struct {
	Waypoint
	Crossed bool `json:"crossed"`
}

// GetJourney returns or creates the journey for a user.
func GetJourney(ctx context.Context, db *sql.DB, userID int64) (*JourneyResponse, error) {
	j, err := getOrCreateJourney(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	return buildJourneyResponse(j), nil
}

// ChangeTheme updates the journey theme for a user. The user's progress is preserved.
// If no journey exists yet for the user, one is created with the given theme.
func ChangeTheme(ctx context.Context, db *sql.DB, userID int64, themeKey string) (*JourneyResponse, error) {
	if !validThemes[themeKey] {
		return nil, fmt.Errorf("invalid theme: %s", themeKey)
	}
	// Ensure the row exists first, then update the theme.
	if _, err := getOrCreateJourney(ctx, db, userID); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `
		UPDATE story_journeys SET theme = ?, updated_at = ? WHERE user_id = ?
	`, themeKey, now, userID)
	if err != nil {
		return nil, fmt.Errorf("update journey theme: %w", err)
	}
	return GetJourney(ctx, db, userID)
}

// UpdateJourneyDistance adds distance to a user's journey and returns any newly crossed waypoints.
// For each newly crossed waypoint a +10 star bonus is awarded and a push notification is sent.
func UpdateJourneyDistance(ctx context.Context, db *sql.DB, userID int64, workoutID int64, distanceM float64) ([]Waypoint, error) {
	if distanceM <= 0 {
		return nil, nil
	}

	j, err := getOrCreateJourney(ctx, db, userID)
	if err != nil {
		return nil, err
	}

	previousKm := j.TotalDistanceM / 1000
	newKm := (j.TotalDistanceM + distanceM) / 1000

	theme, ok := themes[j.Theme]
	if !ok {
		theme = themes["middle_earth"]
	}

	// Find newly crossed waypoints (excluding the start at 0km).
	var crossed []Waypoint
	for _, wp := range theme.Waypoints {
		if wp.DistanceKm == 0 {
			continue
		}
		if previousKm < wp.DistanceKm && newKm >= wp.DistanceKm {
			crossed = append(crossed, wp)
		}
	}

	// Update distance in DB.
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, `
		UPDATE story_journeys
		SET total_distance_m = total_distance_m + ?, updated_at = ?
		WHERE user_id = ?
	`, distanceM, now, userID)
	if err != nil {
		return nil, fmt.Errorf("update journey distance: %w", err)
	}

	// Award stars and send notifications for each crossed waypoint.
	for _, wp := range crossed {
		award := StarAward{
			Amount:      10,
			Reason:      "waypoint_reached",
			Description: fmt.Sprintf("Reached %s on your journey!", wp.Name),
		}
		if recErr := recordAwards(db, userID, workoutID, []StarAward{award}); recErr != nil {
			log.Printf("stars: waypoint award for user %d waypoint %q: %v", userID, wp.Name, recErr)
		}
		go sendWaypointNotifications(db, userID, wp, theme)
	}

	return crossed, nil
}

// getOrCreateJourney returns the user's journey row, creating one with the default theme if missing.
func getOrCreateJourney(ctx context.Context, db *sql.DB, userID int64) (*Journey, error) {
	var j Journey
	err := db.QueryRowContext(ctx, `
		SELECT id, user_id, theme, total_distance_m, created_at, updated_at
		FROM story_journeys WHERE user_id = ?
	`, userID).Scan(&j.ID, &j.UserID, &j.Theme, &j.TotalDistanceM, &j.CreatedAt, &j.UpdatedAt)
	if err == sql.ErrNoRows {
		now := time.Now().UTC().Format(time.RFC3339)
		res, insErr := db.ExecContext(ctx, `
			INSERT INTO story_journeys (user_id, theme, total_distance_m, created_at, updated_at)
			VALUES (?, 'middle_earth', 0, ?, ?)
		`, userID, now, now)
		if insErr != nil {
			return nil, fmt.Errorf("create journey: %w", insErr)
		}
		id, _ := res.LastInsertId()
		j = Journey{ID: id, UserID: userID, Theme: "middle_earth", TotalDistanceM: 0, CreatedAt: now, UpdatedAt: now}
		return &j, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get journey: %w", err)
	}
	return &j, nil
}

// buildJourneyResponse converts a Journey row into a full JourneyResponse.
func buildJourneyResponse(j *Journey) *JourneyResponse {
	theme, ok := themes[j.Theme]
	if !ok {
		theme = themes["middle_earth"]
	}

	distKm := j.TotalDistanceM / 1000

	// Find current and next waypoints based on distance traveled.
	var currentWP Waypoint
	var nextWP *Waypoint
	for i, wp := range theme.Waypoints {
		if distKm >= wp.DistanceKm {
			currentWP = wp
			if i+1 < len(theme.Waypoints) {
				next := theme.Waypoints[i+1]
				nextWP = &next
			}
		}
	}

	// Progress within the current leg.
	var progressPct, distToNext float64
	var legDesc string
	if nextWP != nil {
		legLen := nextWP.DistanceKm - currentWP.DistanceKm
		traveled := distKm - currentWP.DistanceKm
		if legLen > 0 {
			progressPct = traveled / legLen * 100
		}
		distToNext = nextWP.DistanceKm - distKm
		legDesc = fmt.Sprintf("Traveling from %s to %s", currentWP.Name, nextWP.Name)
	} else {
		progressPct = 100
		legDesc = fmt.Sprintf("You've completed the journey to %s!", currentWP.Name)
	}

	// Build waypoint status list.
	statuses := make([]WaypointStatus, len(theme.Waypoints))
	for i, wp := range theme.Waypoints {
		statuses[i] = WaypointStatus{
			Waypoint: wp,
			Crossed:  distKm >= wp.DistanceKm,
		}
	}

	// Build available themes list (slice for consistent ordering).
	availableThemes := []Theme{themes["middle_earth"], themes["space"], themes["pirate"]}

	return &JourneyResponse{
		Theme:              theme.Key,
		ThemeName:          theme.Name,
		ThemeEmoji:         theme.Emoji,
		TotalDistanceM:     j.TotalDistanceM,
		TotalJourneyDistKm: theme.TotalDistanceKm,
		CurrentWaypoint:    currentWP,
		NextWaypoint:       nextWP,
		LegDescription:     legDesc,
		ProgressInLegPct:   progressPct,
		DistanceToNextKm:   distToNext,
		Waypoints:          statuses,
		AvailableThemes:    availableThemes,
	}
}

// sendWaypointNotifications sends a push notification to the child and parent when a waypoint is crossed.
func sendWaypointNotifications(db *sql.DB, childID int64, wp Waypoint, theme Theme) {
	childPayload := push.Notification{
		Title: fmt.Sprintf("%s Reached! %s", wp.Name, wp.Emoji),
		Body:  wp.Description,
		Tag:   "waypoint-reached",
	}
	childBytes, err := json.Marshal(childPayload)
	if err != nil {
		log.Printf("stars: marshal waypoint push payload: %v", err)
		return
	}
	if _, err := push.SendToUser(db, pushClient, childID, childBytes); err != nil {
		log.Printf("stars: send waypoint push to child %d: %v", childID, err)
	}

	link, err := family.GetParent(db, childID)
	if err != nil {
		log.Printf("stars: get parent for child %d (waypoint): %v", childID, err)
		return
	}
	if link == nil {
		return
	}
	nickname := link.Nickname
	if nickname == "" {
		nickname = "Your child"
	}
	parentPayload := push.Notification{
		Title: fmt.Sprintf("%s reached %s! %s", nickname, wp.Name, wp.Emoji),
		Body:  fmt.Sprintf("%s is on the %s journey and just arrived at %s!", nickname, theme.Name, wp.Name),
		Tag:   "waypoint-reached",
	}
	parentBytes, err := json.Marshal(parentPayload)
	if err != nil {
		log.Printf("stars: marshal parent waypoint push payload: %v", err)
		return
	}
	if _, err := push.SendToUser(db, pushClient, link.ParentID, parentBytes); err != nil {
		log.Printf("stars: send waypoint push to parent %d: %v", link.ParentID, err)
	}
}

// GetJourneyHandler handles GET /api/stars/journey.
func GetJourneyHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		resp, err := GetJourney(r.Context(), db, user.ID)
		if err != nil {
			log.Printf("stars: GetJourney user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load journey"})
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// ChangeThemeHandler handles PUT /api/stars/journey/theme.
func ChangeThemeHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var body struct {
			Theme string `json:"theme"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		resp, err := ChangeTheme(r.Context(), db, user.ID, body.Theme)
		if err != nil {
			if err.Error() == fmt.Sprintf("invalid theme: %s", body.Theme) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			log.Printf("stars: ChangeTheme user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to change theme"})
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
