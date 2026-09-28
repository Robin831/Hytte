package livestream

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/quiethours"
)

// PushFunc delivers one notification payload to a user. Swappable in tests.
type PushFunc func(db *sql.DB, userID int64, payload []byte) error

func defaultPush(db *sql.DB, userID int64, payload []byte) error {
	_, err := push.SendToUser(db, push.DefaultHTTPClient, userID, payload)
	return err
}

// notifyGoLive pushes "<name> is live" to every other user who can watch
// (has the livestream feature, admins included), skipping anyone in quiet
// hours. Runs in the background; errors are only logged.
func notifyGoLive(db *sql.DB, send PushFunc, s *Session) {
	users, err := auth.GetAllUsersFeatures(db)
	if err != nil {
		log.Printf("livestream: notify: list users: %v", err)
		return
	}
	name := strings.TrimSpace(s.OwnerName)
	if name == "" {
		name = "Someone"
	}
	body := "Tap to watch"
	if s.Title != "" {
		body = s.Title + " — tap to watch"
	}
	payload, err := json.Marshal(push.Notification{
		Title:   name + " is live",
		Body:    body,
		URL:     fmt.Sprintf("/live/%d", s.ID),
		Tag:     fmt.Sprintf("livestream-%d", s.ID),
		Urgency: "high",
		TTL:     600, // a stale "is live" is useless after ~10 minutes
	})
	if err != nil {
		log.Printf("livestream: notify: marshal: %v", err)
		return
	}
	for _, u := range users {
		if u.UserID == s.UserID || !u.Features["livestream"] {
			continue
		}
		if quiethours.IsActive(db, u.UserID) {
			continue
		}
		if err := send(db, u.UserID, payload); err != nil {
			log.Printf("livestream: notify user %d: %v", u.UserID, err)
		}
	}
}
