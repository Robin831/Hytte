package webhooks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/quiethours"
)

// webhookPushPayload is the JSON structure the Service Worker expects when
// a push notification arrives for a new webhook request.
type webhookPushPayload struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	URL       string `json:"url"`
	Icon      string `json:"icon,omitempty"`
	Tag       string `json:"tag"`
	Timestamp int64  `json:"timestamp"`
}

// iconForSource returns a source-specific icon path, or empty string for
// unrecognised sources or sources without a bundled icon asset.
func iconForSource(_ string) string {
	// No icon assets are currently bundled; return empty so the Service Worker
	// falls back to its own default icon instead of serving a 404.
	return ""
}

// dispatchPushNotifications sends a push notification to the owner of the
// webhook endpoint after a new request arrives. It is designed to run in a
// goroutine — it logs errors and never returns them.
func dispatchPushNotifications(
	ctx context.Context,
	db *sql.DB,
	httpClient *http.Client,
	endpointID string,
	webhookID int64,
	headers map[string]string,
	body []byte,
	method, urlPath string,
) {
	// Derive source and event type from the headers map.
	// Go's http package stores headers in canonical form (e.g. "X-Github-Event"),
	// but we also do a case-insensitive fallback for robustness.
	source := ""
	eventType := ""

	githubEvent := headers["X-Github-Event"]
	if githubEvent == "" {
		for k, v := range headers {
			if strings.EqualFold(k, "x-github-event") {
				githubEvent = v
				break
			}
		}
	}
	if githubEvent != "" {
		source = "github"
		eventType = githubEvent
	}

	// Detect Forge webhooks via X-Forge-Event header.
	if source == "" {
		forgeEvent := headers["X-Forge-Event"]
		if forgeEvent == "" {
			for k, v := range headers {
				if strings.EqualFold(k, "x-forge-event") {
					forgeEvent = v
					break
				}
			}
		}
		if forgeEvent != "" {
			source = "forge"
			eventType = forgeEvent
		}
	}

	title, notifBody := FormatWebhookNotification(headers, body, method, urlPath)

	payload := webhookPushPayload{
		Title:     title,
		Body:      notifBody,
		URL:       fmt.Sprintf("/webhooks#%s", endpointID),
		Icon:      iconForSource(source),
		Tag:       fmt.Sprintf("webhook-%d", webhookID),
		Timestamp: time.Now().Unix(),
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		slog.Error("webhook push: marshal payload", "err", err)
		return
	}

	// Look up the endpoint owner to direct the notification.
	var ownerID int64
	err = db.QueryRowContext(ctx, "SELECT user_id FROM webhook_endpoints WHERE id = ?", endpointID).Scan(&ownerID)
	if err != nil {
		slog.Error("webhook push: lookup endpoint owner", "endpointID", endpointID, "err", err)
		return
	}

	// Fetch user preferences once for both quiet-hours and filter checks,
	// avoiding duplicate DB queries.
	prefs, err := auth.GetPreferences(db, ownerID)
	if err != nil {
		slog.Error("webhook push: fetch preferences", "userID", ownerID, "err", err)
		// Fail open — deliver the notification rather than silently dropping it.
		prefs = nil
	}

	// Skip notification delivery during the user's quiet hours.
	if quiethours.IsActiveWithPrefs(prefs) {
		slog.Debug("webhook push: skipped during quiet hours", "userID", ownerID, "endpointID", endpointID)
		return
	}

	// Check notification filters — skip if source or event type is disabled.
	if isFilteredOut(prefs, source, eventType) {
		slog.Debug("webhook push: filtered out by user preferences", "userID", ownerID, "source", source, "event", eventType)
		return
	}

	results, err := push.SendToUser(db, httpClient, ownerID, payloadBytes)
	if err != nil {
		slog.Error("webhook push: send to user", "userID", ownerID, "err", err)
		return
	}

	// Determine whether all subscriptions are permanently dead (410/404).
	// Network errors (StatusCode == 0) are transient and must not count as
	// evidence of death — if every result is a network error, we must NOT
	// mark the user degraded.
	allDead := true
	seenDefinitive := false
	for _, r := range results {
		if r.Err != nil {
			slog.Error("webhook push: delivery failed", "subscriptionID", r.SubscriptionID, "err", r.Err)
		}
		if r.StatusCode == 0 {
			continue
		}
		seenDefinitive = true
		if r.StatusCode != http.StatusGone && r.StatusCode != http.StatusNotFound {
			allDead = false
		}
	}

	// Only mark degraded when at least one subscription gave a definitive
	// HTTP response and all such responses indicated permanent death.
	if seenDefinitive && allDead {
		if err := auth.SetPreference(db, ownerID, "notifications_degraded", "true"); err != nil {
			slog.Error("webhook push: mark notifications degraded", "userID", ownerID, "err", err)
		}
	}
}
