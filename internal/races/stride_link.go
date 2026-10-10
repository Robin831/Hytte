package races

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/stride"
)

// A catalog race the user is going to run can be added to Stride, where it
// shapes the training plan. race_watch.stride_race_id remembers the link so
// the page can show it and catalog date changes can follow into Stride.

var (
	ErrNoStride      = errors.New("stride is not enabled for this user")
	ErrAlreadyLinked = errors.New("this race is already in Stride")
	ErrNotLinked     = errors.New("this race is not linked to Stride")
	validStridePrio  = set("A", "B", "C")
	maxTargetSeconds = 24 * 3600
)

// StrideLinkInput is the body of POST /api/races/{id}/stride.
type StrideLinkInput struct {
	Priority   string `json:"priority"`
	TargetTime *int   `json:"target_time"` // seconds
}

func hasFeature(db *sql.DB, userID int64, feature string) (bool, error) {
	var admin bool
	if err := db.QueryRow(`SELECT is_admin FROM users WHERE id = ?`, userID).Scan(&admin); err != nil {
		return false, err
	}
	features, err := auth.GetUserFeatures(db, userID, admin)
	if err != nil {
		return false, err
	}
	return features[feature], nil
}

// LinkToStride creates a Stride race for the user from a catalog race and
// links it to their watch (creating a "registered" watch if they had none).
func LinkToStride(ctx context.Context, db *sql.DB, userID, eventID int64, in StrideLinkInput) (*Watch, *stride.Race, error) {
	in.Priority = strings.ToUpper(strings.TrimSpace(in.Priority))
	if in.Priority == "" {
		in.Priority = "B"
	}
	if !validStridePrio[in.Priority] {
		return nil, nil, invalid("priority must be A, B or C")
	}
	if in.TargetTime != nil && (*in.TargetTime <= 0 || *in.TargetTime > maxTargetSeconds) {
		return nil, nil, invalid("target_time must be a positive number of seconds")
	}
	ok, err := hasFeature(db, userID, "stride")
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, ErrNoStride
	}
	event, err := GetEvent(ctx, db, eventID)
	if err != nil {
		return nil, nil, err
	}

	watch, err := GetWatch(ctx, db, userID, eventID)
	switch {
	case errors.Is(err, ErrNotFound):
		if watch, err = SetWatch(ctx, db, userID, eventID, WatchInput{State: "registered"}); err != nil {
			return nil, nil, err
		}
	case err != nil:
		return nil, nil, err
	}
	if watch.StrideRaceID != nil {
		if _, err := stride.GetRaceByID(db, *watch.StrideRaceID, userID); err == nil {
			return nil, nil, ErrAlreadyLinked
		}
		// The Stride race was deleted there: link afresh.
	}

	notes := "Fra løpsoversikten"
	if event.URL != "" {
		notes += ": " + event.URL
	}
	sr, err := stride.CreateRace(db, userID, event.Name, event.RaceDate, float64(event.DistanceM), in.TargetTime, in.Priority, notes)
	if err != nil {
		return nil, nil, fmt.Errorf("create stride race: %w", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE race_watch SET stride_race_id = ?, updated_at = ? WHERE user_id = ? AND event_id = ?`,
		sr.ID, now(), userID, eventID); err != nil {
		return nil, nil, fmt.Errorf("link stride race: %w", err)
	}
	if err := stride.MarkMacroPlansStaleForRaces(ctx, db, userID, event.RaceDate); err != nil {
		log.Printf("races: mark stride plan stale for user %d: %v", userID, err)
	}
	watch, err = GetWatch(ctx, db, userID, eventID)
	return watch, sr, err
}

// UnlinkFromStride removes the link; with deleteRace it also deletes the
// Stride race (and so takes it out of the training plan).
func UnlinkFromStride(ctx context.Context, db *sql.DB, userID, eventID int64, deleteRace bool) error {
	watch, err := GetWatch(ctx, db, userID, eventID)
	if err != nil {
		return err
	}
	if watch.StrideRaceID == nil {
		return ErrNotLinked
	}
	if deleteRace {
		if sr, err := stride.GetRaceByID(db, *watch.StrideRaceID, userID); err == nil {
			if err := stride.DeleteRace(db, sr.ID, userID); err != nil {
				return fmt.Errorf("delete stride race: %w", err)
			}
			if err := stride.MarkMacroPlansStaleForRaces(ctx, db, userID, sr.Date); err != nil {
				log.Printf("races: mark stride plan stale for user %d: %v", userID, err)
			}
		}
	}
	_, err = db.ExecContext(ctx, `UPDATE race_watch SET stride_race_id = NULL, updated_at = ? WHERE user_id = ? AND event_id = ?`,
		now(), userID, eventID)
	return err
}

// syncStrideDates moves linked Stride races when a catalog race's date
// changes, so the training plan follows a rescheduled race. Failures are
// logged: the catalog edit itself has already succeeded.
func syncStrideDates(ctx context.Context, db *sql.DB, eventID int64, oldDate, newDate string) {
	rows, err := db.QueryContext(ctx, `SELECT user_id, stride_race_id FROM race_watch WHERE event_id = ? AND stride_race_id IS NOT NULL`, eventID)
	if err != nil {
		log.Printf("races: stride date sync for race %d: %v", eventID, err)
		return
	}
	type link struct{ userID, raceID int64 }
	var links []link
	for rows.Next() {
		var l link
		if err := rows.Scan(&l.userID, &l.raceID); err == nil {
			links = append(links, l)
		}
	}
	rows.Close()
	for _, l := range links {
		sr, err := stride.GetRaceByID(db, l.raceID, l.userID)
		if err != nil || sr.Date != oldDate {
			continue // gone, or the user moved it in Stride themselves
		}
		if _, err := stride.UpdateRace(db, sr.ID, l.userID, sr.Name, newDate, sr.DistanceM, sr.TargetTime, sr.Priority, sr.Notes, sr.ResultTime); err != nil {
			log.Printf("races: move stride race %d for user %d: %v", sr.ID, l.userID, err)
			continue
		}
		if err := stride.MarkMacroPlansStaleForRaces(ctx, db, l.userID, oldDate, newDate); err != nil {
			log.Printf("races: mark stride plan stale for user %d: %v", l.userID, err)
		}
	}
}
