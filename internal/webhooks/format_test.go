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

func TestFormatWebhookNotification_ForgeSummaryPreferredOverMessage(t *testing.T) {
	// When both summary and message are present, summary should be used.
	headers := map[string]string{}
	body, _ := json.Marshal(map[string]any{
		"event_type": "pr_ready_to_merge",
		"bead_id":    "ext-53",
		"anvil":      "hytte",
		"summary":    "Add OAuth2 login support",
		"message":    "PR #53 ready to merge: https://github.com/Robin831/Hytte/pull/53",
	})

	_, notifBody := FormatWebhookNotification(headers, body, "POST", "/api/hooks/abc")

	want := "Add OAuth2 login support (ext-53, hytte)"
	if notifBody != want {
		t.Errorf("body = %q, want %q", notifBody, want)
	}
}

func TestFormatWebhookNotification_ForgeFallsBackToMessageWhenSummaryEmpty(t *testing.T) {
	// When summary is absent/empty, message should be used.
	headers := map[string]string{}
	body, _ := json.Marshal(map[string]any{
		"event_type": "pr_ready_to_merge",
		"bead_id":    "ext-53",
		"anvil":      "hytte",
		"summary":    "",
		"message":    "PR #53 ready to merge: https://github.com/Robin831/Hytte/pull/53",
	})

	_, notifBody := FormatWebhookNotification(headers, body, "POST", "/api/hooks/abc")

	want := "PR #53 ready to merge: https://github.com/Robin831/Hytte/pull/53 (ext-53, hytte)"
	if notifBody != want {
		t.Errorf("body = %q, want %q", notifBody, want)
	}
}

func TestFormatWebhookNotification_RadarrEvents(t *testing.T) {
	tests := []struct {
		name      string
		payload   map[string]any
		wantTitle string
		wantBody  string
	}{
		{
			name: "Test",
			payload: map[string]any{
				"eventType":    "Test",
				"instanceName": "Radarr",
			},
			wantTitle: "Radarr: Test",
			wantBody:  "Test notification from Radarr",
		},
		{
			name: "Grab",
			payload: map[string]any{
				"eventType":    "Grab",
				"instanceName": "Radarr",
				"movie":        map[string]any{"title": "Inception", "year": float64(2010)},
				"release":      map[string]any{"quality": "Bluray-1080p", "indexer": "NZBgeek"},
			},
			wantTitle: "Radarr: Grabbed",
			wantBody:  "Inception (2010) — Bluray-1080p from NZBgeek",
		},
		{
			name: "Download",
			payload: map[string]any{
				"eventType":    "Download",
				"instanceName": "Radarr",
				"movie":        map[string]any{"title": "Inception", "year": float64(2010)},
				"release":      map[string]any{"quality": "Bluray-1080p"},
			},
			wantTitle: "Radarr: Downloaded",
			wantBody:  "Inception (2010) — Bluray-1080p",
		},
		{
			name: "Rename",
			payload: map[string]any{
				"eventType":    "Rename",
				"instanceName": "Radarr",
				"movie":        map[string]any{"title": "Inception", "year": float64(2010)},
			},
			wantTitle: "Radarr: Renamed",
			wantBody:  "Inception (2010)",
		},
		{
			name: "MovieAdded",
			payload: map[string]any{
				"eventType":    "MovieAdded",
				"instanceName": "Radarr",
				"movie":        map[string]any{"title": "The Matrix", "year": float64(1999)},
			},
			wantTitle: "Radarr: Movie Added",
			wantBody:  "The Matrix (1999)",
		},
		{
			name: "MovieDelete",
			payload: map[string]any{
				"eventType":    "MovieDelete",
				"instanceName": "Radarr",
				"movie":        map[string]any{"title": "The Matrix", "year": float64(1999)},
			},
			wantTitle: "Radarr: Movie Deleted",
			wantBody:  "The Matrix (1999)",
		},
		{
			name: "MovieFileDelete",
			payload: map[string]any{
				"eventType":    "MovieFileDelete",
				"instanceName": "Radarr",
				"movie":        map[string]any{"title": "The Matrix", "year": float64(1999)},
			},
			wantTitle: "Radarr: File Deleted",
			wantBody:  "The Matrix (1999)",
		},
		{
			name: "Health",
			payload: map[string]any{
				"eventType":    "Health",
				"instanceName": "Radarr",
				"message":      "Indexer NZBgeek is unavailable",
			},
			wantTitle: "Radarr: Health Issue",
			wantBody:  "Indexer NZBgeek is unavailable",
		},
		{
			name: "HealthRestored",
			payload: map[string]any{
				"eventType":    "HealthRestored",
				"instanceName": "Radarr",
				"message":      "Indexer NZBgeek is available again",
			},
			wantTitle: "Radarr: Health Restored",
			wantBody:  "Indexer NZBgeek is available again",
		},
		{
			name: "ApplicationUpdate",
			payload: map[string]any{
				"eventType":    "ApplicationUpdate",
				"instanceName": "Radarr",
				"version":      "5.3.6.8612",
			},
			wantTitle: "Radarr: Updated",
			wantBody:  "Updated to 5.3.6.8612",
		},
		{
			name: "ManualInteractionRequired",
			payload: map[string]any{
				"eventType":    "ManualInteractionRequired",
				"instanceName": "Radarr",
				"movie":        map[string]any{"title": "Inception"},
			},
			wantTitle: "Radarr: Manual Action Needed",
			wantBody:  "Inception — manual interaction required",
		},
		{
			name: "UnknownEventType",
			payload: map[string]any{
				"eventType":    "SomeFutureEvent",
				"instanceName": "Radarr",
			},
			wantTitle: "Radarr: SomeFutureEvent",
			wantBody:  "",
		},
		{
			name: "MissingMovieFields",
			payload: map[string]any{
				"eventType":    "Grab",
				"instanceName": "Radarr",
			},
			wantTitle: "Radarr: Grabbed",
			wantBody:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.payload)
			title, notifBody := FormatWebhookNotification(map[string]string{}, body, "POST", "/hooks/abc")
			if title != tt.wantTitle {
				t.Errorf("title = %q, want %q", title, tt.wantTitle)
			}
			if notifBody != tt.wantBody {
				t.Errorf("body = %q, want %q", notifBody, tt.wantBody)
			}
		})
	}
}

func TestFormatWebhookNotification_SonarrEvents(t *testing.T) {
	tests := []struct {
		name      string
		payload   map[string]any
		wantTitle string
		wantBody  string
	}{
		{
			name: "Test",
			payload: map[string]any{
				"eventType":    "Test",
				"instanceName": "Sonarr",
			},
			wantTitle: "Sonarr: Test",
			wantBody:  "Test notification from Sonarr",
		},
		{
			name: "Grab",
			payload: map[string]any{
				"eventType":    "Grab",
				"instanceName": "Sonarr",
				"series":       map[string]any{"title": "Breaking Bad", "year": float64(2008)},
				"episodes": []any{
					map[string]any{"seasonNumber": float64(1), "episodeNumber": float64(3), "title": "...And the Bag's in the River"},
				},
				"release": map[string]any{"quality": "HDTV-720p"},
			},
			wantTitle: "Sonarr: Grabbed",
			wantBody:  "Breaking Bad S01E03 — HDTV-720p",
		},
		{
			name: "Download",
			payload: map[string]any{
				"eventType":    "Download",
				"instanceName": "Sonarr",
				"series":       map[string]any{"title": "Breaking Bad", "year": float64(2008)},
				"episodes": []any{
					map[string]any{"seasonNumber": float64(1), "episodeNumber": float64(3), "title": "...And the Bag's in the River"},
				},
			},
			wantTitle: "Sonarr: Downloaded",
			wantBody:  "Breaking Bad S01E03 '...And the Bag's in the River'",
		},
		{
			name: "Rename",
			payload: map[string]any{
				"eventType":    "Rename",
				"instanceName": "Sonarr",
				"series":       map[string]any{"title": "Breaking Bad"},
			},
			wantTitle: "Sonarr: Renamed",
			wantBody:  "Breaking Bad",
		},
		{
			name: "SeriesAdd",
			payload: map[string]any{
				"eventType":    "SeriesAdd",
				"instanceName": "Sonarr",
				"series":       map[string]any{"title": "Breaking Bad", "year": float64(2008)},
			},
			wantTitle: "Sonarr: Series Added",
			wantBody:  "Breaking Bad (2008)",
		},
		{
			name: "SeriesDelete",
			payload: map[string]any{
				"eventType":    "SeriesDelete",
				"instanceName": "Sonarr",
				"series":       map[string]any{"title": "Breaking Bad"},
			},
			wantTitle: "Sonarr: Series Deleted",
			wantBody:  "Breaking Bad",
		},
		{
			name: "EpisodeFileDelete",
			payload: map[string]any{
				"eventType":    "EpisodeFileDelete",
				"instanceName": "Sonarr",
				"series":       map[string]any{"title": "Breaking Bad"},
				"episodes": []any{
					map[string]any{"seasonNumber": float64(2), "episodeNumber": float64(10)},
				},
			},
			wantTitle: "Sonarr: File Deleted",
			wantBody:  "Breaking Bad S02E10",
		},
		{
			name: "Health",
			payload: map[string]any{
				"eventType":    "Health",
				"instanceName": "Sonarr",
				"message":      "Download client is unavailable",
			},
			wantTitle: "Sonarr: Health Issue",
			wantBody:  "Download client is unavailable",
		},
		{
			name: "HealthRestored",
			payload: map[string]any{
				"eventType":    "HealthRestored",
				"instanceName": "Sonarr",
				"message":      "Download client is available again",
			},
			wantTitle: "Sonarr: Health Restored",
			wantBody:  "Download client is available again",
		},
		{
			name: "ApplicationUpdate",
			payload: map[string]any{
				"eventType":    "ApplicationUpdate",
				"instanceName": "Sonarr",
				"version":      "4.0.1.929",
			},
			wantTitle: "Sonarr: Updated",
			wantBody:  "Updated to 4.0.1.929",
		},
		{
			name: "ManualInteractionRequired",
			payload: map[string]any{
				"eventType":    "ManualInteractionRequired",
				"instanceName": "Sonarr",
				"series":       map[string]any{"title": "Breaking Bad"},
			},
			wantTitle: "Sonarr: Manual Action Needed",
			wantBody:  "Breaking Bad — manual interaction required",
		},
		{
			name: "UnknownEventType",
			payload: map[string]any{
				"eventType":    "SomeFutureEvent",
				"instanceName": "Sonarr",
			},
			wantTitle: "Sonarr: SomeFutureEvent",
			wantBody:  "",
		},
		{
			name: "EmptyEpisodesArray",
			payload: map[string]any{
				"eventType":    "Download",
				"instanceName": "Sonarr",
				"series":       map[string]any{"title": "Breaking Bad"},
				"episodes":     []any{},
			},
			wantTitle: "Sonarr: Downloaded",
			wantBody:  "Breaking Bad",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.payload)
			title, notifBody := FormatWebhookNotification(map[string]string{}, body, "POST", "/hooks/abc")
			if title != tt.wantTitle {
				t.Errorf("title = %q, want %q", title, tt.wantTitle)
			}
			if notifBody != tt.wantBody {
				t.Errorf("body = %q, want %q", notifBody, tt.wantBody)
			}
		})
	}
}

func TestFormatWebhookNotification_ArrMissingInstanceName(t *testing.T) {
	// Payload with eventType but no instanceName should NOT match *arr formatter.
	body, _ := json.Marshal(map[string]any{
		"eventType": "Grab",
		"movie":     map[string]any{"title": "Inception"},
	})

	title, _ := FormatWebhookNotification(map[string]string{}, body, "POST", "/hooks/abc")

	// Should fall through to generic — eventType is not in the generic keys list.
	if title == "Radarr: Grabbed" {
		t.Errorf("should not match *arr formatter without instanceName")
	}
}

func TestFormatWebhookNotification_ArrDoesNotConflictWithForge(t *testing.T) {
	// Forge payloads use event_type (snake_case), not eventType (camelCase).
	body, _ := json.Marshal(map[string]any{
		"event_type": "pr_created",
		"message":    "PR created",
	})

	title, _ := FormatWebhookNotification(map[string]string{}, body, "POST", "/hooks/abc")

	if title != "Forge: PR Created" {
		t.Errorf("Forge payload incorrectly handled, title = %q", title)
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
