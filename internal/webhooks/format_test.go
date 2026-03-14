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
