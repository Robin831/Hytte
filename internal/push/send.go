package push

import (
	"database/sql"
	"encoding/json"
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
	subs, err := GetSubscriptionsByUserID(db, userID)
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

	vapidPrivateKey := os.Getenv("VAPID_PRIVATE_KEY")
	vapidPublicKey := os.Getenv("VAPID_PUBLIC_KEY")
	vapidSubject := os.Getenv("VAPID_SUBJECT")

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
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusGone {
			log.Printf("push subscription expired (410), removing endpoint: %s", sub.Endpoint)
			if err := DeleteSubscriptionByEndpoint(db, sub.Endpoint); err != nil {
				log.Printf("failed to delete expired subscription: %v", err)
			}
		} else if resp.StatusCode == http.StatusTooManyRequests {
			log.Printf("push rate limited (429) for endpoint: %s", sub.Endpoint)
		} else if resp.StatusCode >= 400 {
			log.Printf("push failed with status %d for endpoint: %s", resp.StatusCode, sub.Endpoint)
		}
	}

	return nil
}
