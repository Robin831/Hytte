package familychat

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/push"
)

// PushSenderFunc delivers a marshaled webpush payload to every push
// subscription owned by userID. Production implementations are expected to
// clean up subscriptions that respond with 410 Gone (the internal/push
// package's SendToUser already does this). Returning an error does not stop
// the wider fan-out: the caller logs and continues.
type PushSenderFunc func(userID int64, payload []byte) error

// defaultPushSender returns a PushSenderFunc backed by the internal/push
// package and the shared push HTTP client. Stale (410 Gone / 404 Not Found)
// subscriptions are deleted automatically by push.SendToUser.
func defaultPushSender(db *sql.DB) PushSenderFunc {
	return func(userID int64, payload []byte) error {
		_, err := push.SendToUser(db, push.DefaultHTTPClient, userID, payload)
		return err
	}
}

// notificationBodyLimit caps the message preview surfaced in the push
// notification body so the banner stays within typical OS limits.
const notificationBodyLimit = 80

// truncate clips s to at most n runes (not bytes), appending an ellipsis
// when truncation occurs so the recipient can tell the message continues.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// notifyOfflineRecipients fans out a webpush notification to every member of
// convID except the sender who is not currently subscribed via SSE for that
// conversation. Per-recipient errors are logged and swallowed so one stale
// or slow subscription cannot block delivery to the others. Returns once
// every recipient has been processed.
func notifyOfflineRecipients(db *sql.DB, hub *Hub, sender PushSenderFunc, convID int64, msg *Message, senderName string) {
	if msg == nil {
		return
	}
	members, err := listMemberIDs(db, convID)
	if err != nil {
		log.Printf("familychat: notify: list members conv=%d: %v", convID, err)
		return
	}

	body := truncate(msg.Body, notificationBodyLimit)
	if body == "" && msg.AttachmentPath != "" {
		body = "Sent an attachment"
	}
	note := push.Notification{
		Title: senderName,
		Body:  body,
		URL:   fmt.Sprintf("/familychat/%d", convID),
		Tag:   fmt.Sprintf("familychat-%d", convID),
	}
	payload, err := json.Marshal(note)
	if err != nil {
		log.Printf("familychat: notify: marshal payload conv=%d: %v", convID, err)
		return
	}

	for _, uid := range members {
		if uid == msg.SenderUserID {
			continue
		}
		if hub.HasSubscriber(uid, convID) {
			continue
		}
		if err := sender(uid, payload); err != nil {
			log.Printf("familychat: notify: send to user=%d conv=%d: %v", uid, convID, err)
		}
	}
}

// senderDisplayName returns a friendly label for the message sender. It
// prefers the stored name and falls back to the email address (used by the
// rare account with an empty display name).
func senderDisplayName(u *auth.User) string {
	if u == nil {
		return ""
	}
	if u.Name != "" {
		return u.Name
	}
	return u.Email
}
