package webhooks

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FormatWebhookNotification parses webhook metadata and body to produce a
// human-readable title and body for a push notification.
//
// Priority:
//  1. GitHub events (X-Github-Event header)
//  2. Generic JSON with a top-level event/action/type field
//  3. Fallback: "Webhook received: METHOD /path — N bytes"
func FormatWebhookNotification(headers map[string]string, body []byte, method, path string) (title, notifBody string) {
	// GitHub events — Go's http package stores headers in canonical form
	// (e.g. "X-Github-Event"), but we also do a case-insensitive fallback.
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
		return formatGitHubNotification(githubEvent, body)
	}

	// Generic JSON: use the first of event/action/type that has a value.
	if len(body) > 0 {
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err == nil {
			for _, key := range []string{"event", "action", "type"} {
				if val, ok := payload[key].(string); ok && val != "" {
					return "Webhook", val
				}
			}
		}
	}

	// Fallback.
	return "Webhook received", fmt.Sprintf("%s %s — %d bytes", method, path, len(body))
}

// formatGitHubNotification produces a title and body for known GitHub event types.
func formatGitHubNotification(eventType string, body []byte) (title, notifBody string) {
	var payload map[string]any
	json.Unmarshal(body, &payload) //nolint:errcheck // best-effort parsing

	switch eventType {
	case "release":
		action := ""
		if a, ok := payload["action"].(string); ok {
			action = " " + a
		}
		desc := ""
		if release, ok := payload["release"].(map[string]any); ok {
			// Prefer human-readable name over tag.
			if name, ok := release["name"].(string); ok && name != "" {
				desc = name
			} else if tag, ok := release["tag_name"].(string); ok {
				desc = tag
			}
		}
		return "GitHub: Release" + action, desc

	case "push":
		branch := ""
		if ref, ok := payload["ref"].(string); ok {
			branch = strings.TrimPrefix(ref, "refs/heads/")
		}
		commitCount := 0
		if commits, ok := payload["commits"].([]any); ok {
			commitCount = len(commits)
		}
		var commitText string
		if commitCount == 1 {
			commitText = "1 commit"
		} else {
			commitText = fmt.Sprintf("%d commits", commitCount)
		}
		if branch != "" {
			return fmt.Sprintf("GitHub: Push to %s", branch), commitText
		}
		return "GitHub: Push", commitText

	case "pull_request":
		number := 0
		prTitle := ""
		action := ""
		if pr, ok := payload["pull_request"].(map[string]any); ok {
			if n, ok := pr["number"].(float64); ok {
				number = int(n)
			}
			if t, ok := pr["title"].(string); ok {
				prTitle = t
			}
		}
		if a, ok := payload["action"].(string); ok {
			action = a
		}
		if number > 0 && action != "" {
			return fmt.Sprintf("GitHub: PR #%d %s", number, action), prTitle
		}
		return "GitHub: Pull request", prTitle

	default:
		return fmt.Sprintf("GitHub: %s", eventType), ""
	}
}
