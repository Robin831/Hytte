package push

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Notification is the payload sent to the browser's push service.
type Notification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url,omitempty"`
}

// SendPushNotification sends a push notification to all of a user's subscriptions.
// It handles 410 Gone by deleting expired subscriptions from the database.
func SendPushNotification(db *sql.DB, userID int64, title, body, url string) error {
	vapidPrivateKey := os.Getenv("VAPID_PRIVATE_KEY")
	vapidPublicKey := os.Getenv("VAPID_PUBLIC_KEY")
	vapidSubject := os.Getenv("VAPID_SUBJECT")
	if vapidPrivateKey == "" || vapidPublicKey == "" || vapidSubject == "" {
		return fmt.Errorf("VAPID keys not configured: set VAPID_PRIVATE_KEY, VAPID_PUBLIC_KEY, and VAPID_SUBJECT")
	}

	subs, err := GetSubscriptions(db, userID)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(Notification{
		Title: title,
		Body:  body,
		URL:   url,
	})
	if err != nil {
		return err
	}

	var errors []error
	for _, sub := range subs {
		s := &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys: webpush.Keys{
				P256dh: sub.P256dh,
				Auth:   sub.Auth,
			},
		}

		resp, err := webpush.SendNotification(payload, s, &webpush.Options{
			Subscriber:      vapidSubject,
			VAPIDPublicKey:  vapidPublicKey,
			VAPIDPrivateKey: vapidPrivateKey,
			TTL:             60,
		})
		if err != nil {
			log.Printf("push send error for endpoint %s: %v", sub.Endpoint, err)
			errors = append(errors, fmt.Errorf("endpoint %s: %w", sub.Endpoint, err))
			continue
		}

		statusCode := resp.StatusCode
		resp.Body.Close()

		if statusCode == http.StatusGone {
			log.Printf("push subscription expired (410), removing endpoint: %s", sub.Endpoint)
			if err := DeleteSubscriptionByEndpoint(db, sub.Endpoint); err != nil {
				log.Printf("failed to delete expired subscription: %v", err)
			}
		} else if statusCode == http.StatusTooManyRequests {
			log.Printf("push rate limited (429) for endpoint: %s", sub.Endpoint)
		} else if statusCode >= 400 {
			log.Printf("push failed with status %d for endpoint: %s", statusCode, sub.Endpoint)
			errors = append(errors, fmt.Errorf("endpoint %s: HTTP %d", sub.Endpoint, statusCode))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("push notification failed for %d/%d subscriptions", len(errors), len(subs))
	}
	return nil
}
