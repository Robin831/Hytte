package calendar

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
)

// ErrEventGone is returned by UpdateEvent when the event no longer exists in
// Google (the user deleted it), so the caller can create it again.
var ErrEventGone = errors.New("calendar event no longer exists")

// WriteEvent is an event Hytte creates in a user's Google Calendar. Set
// AllDayDate (YYYY-MM-DD) for an all-day event, otherwise Start and End.
type WriteEvent struct {
	Summary     string
	Description string
	Location    string
	AllDayDate  string
	Start, End  time.Time
	// Source tags the event in private extended properties, so Hytte's own
	// events can be told apart from the user's.
	Source string
}

func (ev WriteEvent) toGoogle() *gcal.Event {
	g := &gcal.Event{
		Summary:     ev.Summary,
		Description: ev.Description,
		Location:    ev.Location,
		// Hytte sends its own push reminders; don't add Google's on top.
		Reminders: &gcal.EventReminders{UseDefault: false, ForceSendFields: []string{"UseDefault"}},
	}
	if ev.Source != "" {
		g.ExtendedProperties = &gcal.EventExtendedProperties{Private: map[string]string{"hytte_source": ev.Source}}
	}
	if ev.AllDayDate != "" {
		day, err := time.Parse("2006-01-02", ev.AllDayDate)
		next := ev.AllDayDate
		if err == nil {
			next = day.AddDate(0, 0, 1).Format("2006-01-02")
		}
		g.Start = &gcal.EventDateTime{Date: ev.AllDayDate}
		g.End = &gcal.EventDateTime{Date: next} // end date is exclusive
	} else {
		g.Start = &gcal.EventDateTime{DateTime: ev.Start.Format(time.RFC3339)}
		g.End = &gcal.EventDateTime{DateTime: ev.End.Format(time.RFC3339)}
	}
	return g
}

func isGone(err error) bool {
	var gerr *googleapi.Error
	return errors.As(err, &gerr) && (gerr.Code == http.StatusNotFound || gerr.Code == http.StatusGone)
}

// InsertEvent creates an event and returns its Google id.
func (c *Client) InsertEvent(ctx context.Context, userID int64, calendarID string, ev WriteEvent) (string, error) {
	svc, err := c.service(ctx, userID)
	if err != nil {
		return "", err
	}
	created, err := svc.Events.Insert(calendarID, ev.toGoogle()).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("insert calendar event: %w", err)
	}
	return created.Id, nil
}

// UpdateEvent replaces an event Hytte created earlier.
func (c *Client) UpdateEvent(ctx context.Context, userID int64, calendarID, eventID string, ev WriteEvent) error {
	svc, err := c.service(ctx, userID)
	if err != nil {
		return err
	}
	if _, err := svc.Events.Update(calendarID, eventID, ev.toGoogle()).Context(ctx).Do(); err != nil {
		if isGone(err) {
			return ErrEventGone
		}
		return fmt.Errorf("update calendar event: %w", err)
	}
	return nil
}

// DeleteEvent removes an event Hytte created; one already gone is fine.
func (c *Client) DeleteEvent(ctx context.Context, userID int64, calendarID, eventID string) error {
	svc, err := c.service(ctx, userID)
	if err != nil {
		return err
	}
	if err := svc.Events.Delete(calendarID, eventID).Context(ctx).Do(); err != nil && !isGone(err) {
		return fmt.Errorf("delete calendar event: %w", err)
	}
	return nil
}
