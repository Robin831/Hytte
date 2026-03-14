package webhooks

import (
	"encoding/json"
	"testing"
)

func TestFormatWebhookNotification_GitHubRelease(t *testing.T) {
	headers := map[string]string{"X-Github-Event": "release"}
	body, _ := json.Marshal(map[string]any{
		"action": "published",
		"release": map[string]any{
			"tag_name": "v0.3.1",
			"name":     "Forge v0.3.1",
		},
	})

	title, notifBody := FormatWebhookNotification(headers, body, "POST", "/hooks/abc")

	if title != "GitHub: Release published" {
		t.Errorf("title = %q, want %q", title, "GitHub: Release published")
	}
	if notifBody != "Forge v0.3.1" {
		t.Errorf("body = %q, want %q", notifBody, "Forge v0.3.1")
	}
}

func TestFormatWebhookNotification_GitHubPush(t *testing.T) {
	headers := map[string]string{"X-Github-Event": "push"}
	body, _ := json.Marshal(map[string]any{
		"ref": "refs/heads/main",
		"commits": []any{
			map[string]any{"id": "abc"},
			map[string]any{"id": "def"},
			map[string]any{"id": "ghi"},
		},
	})

	title, notifBody := FormatWebhookNotification(headers, body, "POST", "/hooks/abc")

	if title != "GitHub: Push to main" {
		t.Errorf("title = %q, want %q", title, "GitHub: Push to main")
	}
	if notifBody != "3 commits" {
		t.Errorf("body = %q, want %q", notifBody, "3 commits")
	}
}

func TestFormatWebhookNotification_GitHubPushSingleCommit(t *testing.T) {
	headers := map[string]string{"X-Github-Event": "push"}
	body, _ := json.Marshal(map[string]any{
		"ref":     "refs/heads/feature/login",
		"commits": []any{map[string]any{"id": "abc"}},
	})

	title, notifBody := FormatWebhookNotification(headers, body, "POST", "/hooks/abc")

	if title != "GitHub: Push to feature/login" {
		t.Errorf("title = %q, want %q", title, "GitHub: Push to feature/login")
	}
	if notifBody != "1 commit" {
		t.Errorf("body = %q, want %q", notifBody, "1 commit")
	}
}

func TestFormatWebhookNotification_GitHubPR(t *testing.T) {
	headers := map[string]string{"X-Github-Event": "pull_request"}
	body, _ := json.Marshal(map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number": float64(42),
			"title":  "Fix login bug",
		},
	})

	title, notifBody := FormatWebhookNotification(headers, body, "POST", "/hooks/abc")

	if title != "GitHub: PR #42 opened" {
		t.Errorf("title = %q, want %q", title, "GitHub: PR #42 opened")
	}
	if notifBody != "Fix login bug" {
		t.Errorf("body = %q, want %q", notifBody, "Fix login bug")
	}
}

func TestFormatWebhookNotification_GitHubCaseInsensitiveHeader(t *testing.T) {
	// Verify that alternate capitalizations of the header key still work.
	headers := map[string]string{"X-GitHub-Event": "push"}
	body, _ := json.Marshal(map[string]any{
		"ref":     "refs/heads/main",
		"commits": []any{map[string]any{"id": "abc"}},
	})

	title, _ := FormatWebhookNotification(headers, body, "POST", "/hooks/abc")

	if title != "GitHub: Push to main" {
		t.Errorf("title = %q, want %q", title, "GitHub: Push to main")
	}
}

func TestFormatWebhookNotification_GenericJSONEvent(t *testing.T) {
	headers := map[string]string{}
	body, _ := json.Marshal(map[string]any{
		"event":  "deployment.completed",
		"status": "success",
	})

	title, notifBody := FormatWebhookNotification(headers, body, "POST", "/hooks/abc")

	if title != "Webhook" {
		t.Errorf("title = %q, want %q", title, "Webhook")
	}
	if notifBody != "deployment.completed" {
		t.Errorf("body = %q, want %q", notifBody, "deployment.completed")
	}
}

func TestFormatWebhookNotification_GenericJSONAction(t *testing.T) {
	headers := map[string]string{}
	body, _ := json.Marshal(map[string]any{
		"action": "user.signup",
	})

	title, notifBody := FormatWebhookNotification(headers, body, "POST", "/hooks/abc")

	if title != "Webhook" {
		t.Errorf("title = %q, want %q", title, "Webhook")
	}
	if notifBody != "user.signup" {
		t.Errorf("body = %q, want %q", notifBody, "user.signup")
	}
}

func TestFormatWebhookNotification_Fallback(t *testing.T) {
	headers := map[string]string{}
	body := []byte("not json at all")

	title, notifBody := FormatWebhookNotification(headers, body, "POST", "/hooks/abc")

	if title != "Webhook received" {
		t.Errorf("title = %q, want %q", title, "Webhook received")
	}
	want := "POST /hooks/abc — 15 bytes"
	if notifBody != want {
		t.Errorf("body = %q, want %q", notifBody, want)
	}
}

func TestFormatWebhookNotification_ForgeFullPayload(t *testing.T) {
	headers := map[string]string{}
	body, _ := json.Marshal(map[string]any{
		"event_type": "pr_ready_to_merge",
		"bead_id":    "ext-53",
		"anvil":      "hytte",
		"message":    "PR #53 ready to merge: https://github.com/Robin831/Hytte/pull/53",
		"timestamp":  "2026-03-14T20:16:19Z",
	})

	title, notifBody := FormatWebhookNotification(headers, body, "POST", "/api/hooks/abc")

	if title != "Forge: PR Ready to Merge" {
		t.Errorf("title = %q, want %q", title, "Forge: PR Ready to Merge")
	}
	want := "PR #53 ready to merge: https://github.com/Robin831/Hytte/pull/53 (ext-53, hytte)"
	if notifBody != want {
		t.Errorf("body = %q, want %q", notifBody, want)
	}
}

func TestFormatWebhookNotification_ForgeNoBeadOrAnvil(t *testing.T) {
	headers := map[string]string{}
	body, _ := json.Marshal(map[string]any{
		"event_type": "daily_cost",
		"message":    "Daily cost report: $4.20",
	})

	title, notifBody := FormatWebhookNotification(headers, body, "POST", "/api/hooks/abc")

	if title != "Forge: Daily Cost" {
		t.Errorf("title = %q, want %q", title, "Forge: Daily Cost")
	}
	if notifBody != "Daily cost report: $4.20" {
		t.Errorf("body = %q, want %q", notifBody, "Daily cost report: $4.20")
	}
}

func TestFormatWebhookNotification_ForgeEventTypes(t *testing.T) {
	tests := []struct {
		eventType string
		wantTitle string
	}{
		{"pr_created", "Forge: PR Created"},
		{"bead_failed", "Forge: Bead Failed"},
		{"daily_cost", "Forge: Daily Cost"},
		{"worker_done", "Forge: Worker Done"},
		{"bead_decomposed", "Forge: Bead Decomposed"},
		{"release_published", "Forge: Release Published"},
		{"pr_ready_to_merge", "Forge: PR Ready to Merge"},
		{"release", "Forge: Release"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{
				"event_type": tt.eventType,
				"message":    "test",
			})
			title, _ := FormatWebhookNotification(map[string]string{}, body, "POST", "/api/hooks/abc")
			if title != tt.wantTitle {
				t.Errorf("title = %q, want %q", title, tt.wantTitle)
			}
		})
	}
}

func TestFormatWebhookNotification_ForgeBeadIDOnly(t *testing.T) {
	headers := map[string]string{}
	body, _ := json.Marshal(map[string]any{
		"event_type": "bead_failed",
		"bead_id":    "ext-99",
		"message":    "Build failed",
	})

	_, notifBody := FormatWebhookNotification(headers, body, "POST", "/api/hooks/abc")

	want := "Build failed (ext-99)"
	if notifBody != want {
		t.Errorf("body = %q, want %q", notifBody, want)
	}
}

func TestFormatWebhookNotification_ForgeAnvilOnly(t *testing.T) {
	headers := map[string]string{}
	body, _ := json.Marshal(map[string]any{
		"event_type": "worker_done",
		"anvil":      "hytte",
		"message":    "Worker finished",
	})

	_, notifBody := FormatWebhookNotification(headers, body, "POST", "/api/hooks/abc")

	want := "Worker finished (hytte)"
	if notifBody != want {
		t.Errorf("body = %q, want %q", notifBody, want)
	}
}

func TestFormatWebhookNotification_NoEventTypeFallsThrough(t *testing.T) {
	// A JSON payload without event_type should NOT match the Forge formatter.
	headers := map[string]string{}
	body, _ := json.Marshal(map[string]any{
		"bead_id": "ext-53",
		"anvil":   "hytte",
		"message": "some message",
	})

	title, _ := FormatWebhookNotification(headers, body, "POST", "/api/hooks/abc")

	if title == "Forge: " {
		t.Errorf("should not have matched Forge formatter without event_type")
	}
}

func TestFormatWebhookNotification_ForgeEmptyEventTypeFallsThrough(t *testing.T) {
	// An empty event_type string should NOT route to the Forge formatter.
	headers := map[string]string{}
	body, _ := json.Marshal(map[string]any{
		"event_type": "",
		"message":    "test message",
	})

	title, _ := FormatWebhookNotification(headers, body, "POST", "/api/hooks/abc")

	if title == "Forge: " {
		t.Errorf("empty event_type should not match Forge formatter, got title %q", title)
	}
}

func TestFormatWebhookNotification_FallbackEmptyBody(t *testing.T) {
	headers := map[string]string{}
	body := []byte{}

	title, notifBody := FormatWebhookNotification(headers, body, "GET", "/hooks/xyz")

	if title != "Webhook received" {
		t.Errorf("title = %q, want %q", title, "Webhook received")
	}
	want := "GET /hooks/xyz — 0 bytes"
	if notifBody != want {
		t.Errorf("body = %q, want %q", notifBody, want)
	}
}
