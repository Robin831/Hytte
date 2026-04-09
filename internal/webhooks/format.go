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

	// Forge webhooks — identified by the event_type field.
	if len(body) > 0 {
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err == nil {
			if et, ok := payload["event_type"].(string); ok && et != "" {
				return formatForgeNotification(payload)
			}

			// Radarr/Sonarr (*arr) webhooks — identified by eventType + instanceName.
			if title, body, ok := formatArrNotification(payload); ok {
				return title, body
			}

			// Generic JSON: use the first of event/action/type that has a value.
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

// formatForgeNotification produces a title and body for Forge webhook payloads.
// Forge payloads are identified by the presence of an "event_type" field.
func formatForgeNotification(payload map[string]any) (title, notifBody string) {
	eventType, _ := payload["event_type"].(string)

	// Build title: split on underscore, title-case each word, uppercase known acronyms.
	words := strings.Split(eventType, "_")
	for i, w := range words {
		lower := strings.ToLower(w)
		switch {
		case lower == "pr":
			words[i] = "PR"
		case i > 0 && isMinorWord(lower):
			words[i] = lower
		default:
			if len(w) > 0 {
				words[i] = strings.ToUpper(w[:1]) + w[1:]
			}
		}
	}
	title = "Forge: " + strings.Join(words, " ")

	// Build body: prefer summary (contains PR title) over message when available.
	message, _ := payload["summary"].(string)
	if message == "" {
		message, _ = payload["message"].(string)
	}
	beadID, _ := payload["bead_id"].(string)
	anvil, _ := payload["anvil"].(string)

	var suffix string
	switch {
	case beadID != "" && anvil != "":
		suffix = fmt.Sprintf("(%s, %s)", beadID, anvil)
	case beadID != "":
		suffix = fmt.Sprintf("(%s)", beadID)
	case anvil != "":
		suffix = fmt.Sprintf("(%s)", anvil)
	}
	switch {
	case message != "" && suffix != "":
		notifBody = message + " " + suffix
	case suffix != "":
		notifBody = suffix
	default:
		notifBody = message
	}

	return title, notifBody
}

// isMinorWord returns true for articles, short prepositions, and conjunctions
// that should remain lowercase in title case (unless they are the first word).
func isMinorWord(w string) bool {
	switch w {
	case "a", "an", "the", "and", "but", "or", "nor", "for", "yet", "so",
		"at", "by", "in", "of", "on", "to", "up", "as", "is", "it":
		return true
	}
	return false
}

// formatArrNotification produces a title and body for Radarr/Sonarr webhook payloads.
// These are identified by the presence of both "eventType" and "instanceName" fields.
func formatArrNotification(payload map[string]any) (title, notifBody string, ok bool) {
	eventType, hasEvent := payload["eventType"].(string)
	instanceName, hasInstance := payload["instanceName"].(string)
	if !hasEvent || !hasInstance || eventType == "" || instanceName == "" {
		return "", "", false
	}

	// Determine source: use instanceName for prefix, detect type by movie vs series key.
	isRadarr := strings.EqualFold(instanceName, "Radarr")
	isSonarr := strings.EqualFold(instanceName, "Sonarr")
	if !isRadarr && !isSonarr {
		// Fallback detection by payload shape.
		if _, hasMovie := payload["movie"].(map[string]any); hasMovie {
			isRadarr = true
		} else if _, hasSeries := payload["series"].(map[string]any); hasSeries {
			isSonarr = true
		}
	}

	prefix := instanceName
	if isRadarr {
		prefix = "Radarr"
	} else if isSonarr {
		prefix = "Sonarr"
	}

	if isRadarr {
		title, notifBody = formatRadarrEvent(prefix, eventType, payload)
	} else if isSonarr {
		title, notifBody = formatSonarrEvent(prefix, eventType, payload)
	} else {
		// Unknown *arr instance — generic format.
		title = fmt.Sprintf("%s: %s", instanceName, eventType)
	}

	return title, notifBody, true
}

func formatRadarrEvent(prefix, eventType string, payload map[string]any) (title, body string) {
	movie, _ := payload["movie"].(map[string]any)
	movieTitle := mapStr(movie, "title")
	movieYear := mapFloat(movie, "year")
	release, _ := payload["release"].(map[string]any)
	quality := mapStr(release, "quality")
	indexer := mapStr(release, "indexer")

	movieDesc := movieTitle
	if movieTitle != "" && movieYear > 0 {
		movieDesc = fmt.Sprintf("%s (%d)", movieTitle, int(movieYear))
	}

	switch eventType {
	case "Test":
		instanceName, _ := payload["instanceName"].(string)
		return prefix + ": Test", fmt.Sprintf("Test notification from %s", instanceName)
	case "Grab":
		body = movieDesc
		if quality != "" {
			body += " — " + quality
		}
		if indexer != "" {
			body += " from " + indexer
		}
		return prefix + ": Grabbed", body
	case "Download":
		body = movieDesc
		if quality != "" {
			body += " — " + quality
		}
		return prefix + ": Downloaded", body
	case "Rename":
		return prefix + ": Renamed", movieDesc
	case "MovieAdded":
		return prefix + ": Movie Added", movieDesc
	case "MovieDelete":
		return prefix + ": Movie Deleted", movieDesc
	case "MovieFileDelete":
		return prefix + ": File Deleted", movieDesc
	case "Health":
		return prefix + ": Health Issue", mapStr(payload, "message")
	case "HealthRestored":
		return prefix + ": Health Restored", mapStr(payload, "message")
	case "ApplicationUpdate":
		return prefix + ": Updated", fmt.Sprintf("Updated to %s", mapStr(payload, "version"))
	case "ManualInteractionRequired":
		body = movieTitle
		if body != "" {
			body += " — "
		}
		body += "manual interaction required"
		return prefix + ": Manual Action Needed", body
	default:
		return fmt.Sprintf("%s: %s", prefix, eventType), ""
	}
}

func formatSonarrEvent(prefix, eventType string, payload map[string]any) (title, body string) {
	series, _ := payload["series"].(map[string]any)
	seriesTitle := mapStr(series, "title")
	seriesYear := mapFloat(series, "year")
	release, _ := payload["release"].(map[string]any)
	quality := mapStr(release, "quality")

	// Get first episode from episodes array.
	var epSeason, epNumber int
	var epTitle string
	if episodes, ok := payload["episodes"].([]any); ok && len(episodes) > 0 {
		if ep, ok := episodes[0].(map[string]any); ok {
			epSeason = int(mapFloat(ep, "seasonNumber"))
			epNumber = int(mapFloat(ep, "episodeNumber"))
			epTitle = mapStr(ep, "title")
		}
	}

	seriesDesc := seriesTitle
	if seriesTitle != "" && seriesYear > 0 {
		seriesDesc = fmt.Sprintf("%s (%d)", seriesTitle, int(seriesYear))
	}

	epCode := ""
	if epSeason > 0 || epNumber > 0 {
		epCode = fmt.Sprintf("S%02dE%02d", epSeason, epNumber)
	}

	switch eventType {
	case "Test":
		instanceName, _ := payload["instanceName"].(string)
		return prefix + ": Test", fmt.Sprintf("Test notification from %s", instanceName)
	case "Grab":
		body = seriesTitle
		if epCode != "" {
			body += " " + epCode
		}
		if quality != "" {
			body += " — " + quality
		}
		return prefix + ": Grabbed", body
	case "Download":
		body = seriesTitle
		if epCode != "" {
			body += " " + epCode
		}
		if epTitle != "" {
			body += " '" + epTitle + "'"
		}
		return prefix + ": Downloaded", body
	case "Rename":
		return prefix + ": Renamed", seriesTitle
	case "SeriesAdd":
		return prefix + ": Series Added", seriesDesc
	case "SeriesDelete":
		return prefix + ": Series Deleted", seriesTitle
	case "EpisodeFileDelete":
		body = seriesTitle
		if epCode != "" {
			body += " " + epCode
		}
		return prefix + ": File Deleted", body
	case "Health":
		return prefix + ": Health Issue", mapStr(payload, "message")
	case "HealthRestored":
		return prefix + ": Health Restored", mapStr(payload, "message")
	case "ApplicationUpdate":
		return prefix + ": Updated", fmt.Sprintf("Updated to %s", mapStr(payload, "version"))
	case "ManualInteractionRequired":
		body = seriesTitle
		if body != "" {
			body += " — "
		}
		body += "manual interaction required"
		return prefix + ": Manual Action Needed", body
	default:
		return fmt.Sprintf("%s: %s", prefix, eventType), ""
	}
}

// mapStr safely extracts a string value from a map.
func mapStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

// mapFloat safely extracts a float64 value from a map (JSON numbers).
func mapFloat(m map[string]any, key string) float64 {
	if m == nil {
		return 0
	}
	v, _ := m[key].(float64)
	return v
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
