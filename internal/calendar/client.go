package calendar

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"golang.org/x/oauth2"
	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// Client wraps the Google Calendar API with token management.
type Client struct {
	db *sql.DB
}

// NewClient creates a calendar client backed by the given database for
// token storage and event caching.
func NewClient(db *sql.DB) *Client {
	return &Client{db: db}
}

// tokenSource returns an oauth2.TokenSource that automatically refreshes
// using the stored refresh token. Updated tokens are persisted back to the DB.
func (c *Client) tokenSource(ctx context.Context, userID int64) (oauth2.TokenSource, error) {
	gt, err := auth.LoadGoogleToken(c.db, userID)
	if err != nil {
		return nil, fmt.Errorf("load google token: %w", err)
	}
	if gt == nil {
		return nil, fmt.Errorf("no google token stored for user %d", userID)
	}

	tok := &oauth2.Token{
		AccessToken:  gt.AccessToken,
		RefreshToken: gt.RefreshToken,
		TokenType:    gt.TokenType,
		Expiry:       gt.Expiry,
	}

	cfg := auth.Config()
	ts := cfg.TokenSource(ctx, tok)

	return &persistingTokenSource{
		base:   ts,
		db:     c.db,
		userID: userID,
		prev:   tok,
	}, nil
}

// service returns a Google Calendar service for the given user.
func (c *Client) service(ctx context.Context, userID int64) (*gcal.Service, error) {
	ts, err := c.tokenSource(ctx, userID)
	if err != nil {
		return nil, err
	}
	svc, err := gcal.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, fmt.Errorf("create calendar service: %w", err)
	}
	return svc, nil
}

// ListCalendars returns the user's Google Calendar list.
func (c *Client) ListCalendars(ctx context.Context, userID int64) ([]CalendarInfo, error) {
	svc, err := c.service(ctx, userID)
	if err != nil {
		return nil, err
	}

	list, err := svc.CalendarList.List().Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}

	calendars := make([]CalendarInfo, 0, len(list.Items))
	for _, item := range list.Items {
		calendars = append(calendars, CalendarInfo{
			ID:              item.Id,
			Summary:         item.Summary,
			Description:     item.Description,
			BackgroundColor: item.BackgroundColor,
			ForegroundColor: item.ForegroundColor,
			Primary:         item.Primary,
		})
	}
	return calendars, nil
}

// FetchAndCacheEvents fetches events from Google Calendar for the given
// calendar and time range, using incremental sync when a sync token is
// available. Fetched events are cached in the database.
func (c *Client) FetchAndCacheEvents(ctx context.Context, userID int64, calendarID string, timeMin, timeMax time.Time) error {
	svc, err := c.service(ctx, userID)
	if err != nil {
		return err
	}

	syncToken, err := LoadSyncToken(c.db, userID, calendarID)
	if err != nil {
		return fmt.Errorf("load sync token: %w", err)
	}

	var allEvents []Event
	var deletedIDs []string
	var nextSyncToken string

	if syncToken != "" {
		// Incremental sync using sync token.
		nextSyncToken, allEvents, deletedIDs, err = c.incrementalSync(ctx, svc, calendarID, syncToken)
		if err != nil {
			// Sync token may be invalid; fall back to full sync.
			log.Printf("calendar: incremental sync failed for user %d calendar %s: %v — falling back to full sync", userID, calendarID, err)
			syncToken = ""
		}
	}

	if syncToken == "" {
		// Full sync for the given time range.
		nextSyncToken, allEvents, err = c.fullSync(ctx, svc, calendarID, timeMin, timeMax)
		if err != nil {
			return fmt.Errorf("full sync: %w", err)
		}
		deletedIDs = nil
	}

	if len(allEvents) > 0 {
		if err := UpsertEvents(c.db, userID, allEvents); err != nil {
			return fmt.Errorf("upsert events: %w", err)
		}
	}

	if len(deletedIDs) > 0 {
		if err := DeleteEvents(c.db, userID, calendarID, deletedIDs); err != nil {
			return fmt.Errorf("delete events: %w", err)
		}
	}

	if nextSyncToken != "" {
		if err := SaveSyncToken(c.db, userID, calendarID, nextSyncToken); err != nil {
			return fmt.Errorf("save sync token: %w", err)
		}
	}

	return nil
}

// fullSync fetches all events in the time range, paginating through results.
func (c *Client) fullSync(ctx context.Context, svc *gcal.Service, calendarID string, timeMin, timeMax time.Time) (string, []Event, error) {
	var events []Event
	var syncToken string
	pageToken := ""

	for {
		call := svc.Events.List(calendarID).
			Context(ctx).
			SingleEvents(true).
			OrderBy("startTime").
			TimeMin(timeMin.Format(time.RFC3339)).
			TimeMax(timeMax.Format(time.RFC3339)).
			MaxResults(250)

		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		result, err := call.Do()
		if err != nil {
			return "", nil, err
		}

		for _, item := range result.Items {
			if e, ok := convertEvent(item, calendarID); ok {
				events = append(events, e)
			}
		}

		syncToken = result.NextSyncToken
		pageToken = result.NextPageToken
		if pageToken == "" {
			break
		}
	}

	return syncToken, events, nil
}

// incrementalSync uses a sync token to get only changed events.
func (c *Client) incrementalSync(ctx context.Context, svc *gcal.Service, calendarID, syncToken string) (string, []Event, []string, error) {
	var events []Event
	var deletedIDs []string
	var nextSyncToken string
	pageToken := ""

	for {
		call := svc.Events.List(calendarID).
			Context(ctx).
			SyncToken(syncToken).
			MaxResults(250)

		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		result, err := call.Do()
		if err != nil {
			return "", nil, nil, err
		}

		for _, item := range result.Items {
			if item.Status == "cancelled" {
				deletedIDs = append(deletedIDs, item.Id)
				continue
			}
			if e, ok := convertEvent(item, calendarID); ok {
				events = append(events, e)
			}
		}

		nextSyncToken = result.NextSyncToken
		pageToken = result.NextPageToken
		if pageToken == "" {
			break
		}
	}

	return nextSyncToken, events, deletedIDs, nil
}

// convertEvent transforms a Google Calendar event into our Event model.
// Returns false if the event should be skipped (e.g. no valid time).
func convertEvent(item *gcal.Event, calendarID string) (Event, bool) {
	e := Event{
		ID:          item.Id,
		CalendarID:  calendarID,
		Title:       item.Summary,
		Description: item.Description,
		Location:    item.Location,
		Status:      item.Status,
	}

	if item.ColorId != "" {
		e.Color = item.ColorId
	}

	if item.Start == nil {
		return Event{}, false
	}

	if item.Start.Date != "" {
		// All-day event: dates are in YYYY-MM-DD format.
		e.AllDay = true
		e.StartTime = item.Start.Date
		if item.End != nil {
			e.EndTime = item.End.Date
		} else {
			e.EndTime = item.Start.Date
		}
	} else if item.Start.DateTime != "" {
		e.StartTime = item.Start.DateTime
		if item.End != nil {
			e.EndTime = item.End.DateTime
		} else {
			e.EndTime = item.Start.DateTime
		}
	} else {
		return Event{}, false
	}

	return e, true
}

// persistingTokenSource wraps an oauth2.TokenSource and saves refreshed
// tokens back to the database.
type persistingTokenSource struct {
	base   oauth2.TokenSource
	db     *sql.DB
	userID int64
	prev   *oauth2.Token
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	tok, err := p.base.Token()
	if err != nil {
		return nil, err
	}

	// If the token changed (was refreshed), persist the new one.
	if tok.AccessToken != p.prev.AccessToken {
		gt := &auth.GoogleToken{
			AccessToken:  tok.AccessToken,
			RefreshToken: tok.RefreshToken,
			TokenType:    tok.TokenType,
			Expiry:       tok.Expiry,
		}
		if err := auth.SaveGoogleToken(p.db, p.userID, gt); err != nil {
			log.Printf("calendar: failed to persist refreshed token for user %d: %v", p.userID, err)
		}
		p.prev = tok
	}

	return tok, nil
}
