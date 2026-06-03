package news

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/training"
)

// scoreModel is the cheap, fast model used for relevance classification.
const scoreModel = "claude-haiku-4-5-20251001"

// maxScorePerRefresh bounds how many uncached articles we send to the ranker in
// a single refresh, to keep latency and token cost predictable. Anything beyond
// this keeps its previous/neutral score until a later refresh.
const maxScorePerRefresh = 40

// scoreBackgroundTimeout caps the background ranking CLI call. The Claude CLI
// starts an agent session (loading project context) before answering, so a
// single Haiku call is ~10-20s; this leaves generous headroom.
const scoreBackgroundTimeout = 110 * time.Second

type scoreResult struct {
	Idx    int    `json:"idx"`
	Score  int    `json:"score"`
	Reason string `json:"reason"`
}

// scoreArticles asks Claude to rate each article 0-100 for how likely the user
// wants to read it, given their like/dislike profile. Returns a map keyed by
// article ID. Best-effort: on any error it returns the error and the caller
// falls back to chronological ordering.
func scoreArticles(ctx context.Context, cfg *training.ClaudeConfig, profile Profile, toScore []Article) (map[string]ScoreEntry, error) {
	if len(toScore) == 0 {
		return map[string]ScoreEntry{}, nil
	}
	if len(toScore) > maxScorePerRefresh {
		toScore = toScore[:maxScorePerRefresh]
	}

	// Use the cheap model regardless of the user's configured chat model.
	c := *cfg
	c.Model = scoreModel

	prompt := buildScorePrompt(profile, toScore)

	// The caller owns the deadline (background jobs use scoreBackgroundTimeout).
	raw, err := training.RunPrompt(ctx, &c, prompt)
	if err != nil {
		return nil, err
	}

	results, err := parseScoreJSON(raw)
	if err != nil {
		return nil, err
	}

	out := make(map[string]ScoreEntry, len(results))
	for _, r := range results {
		if r.Idx < 0 || r.Idx >= len(toScore) {
			continue
		}
		score := r.Score
		if score < 0 {
			score = 0
		}
		if score > 100 {
			score = 100
		}
		reason := r.Reason
		if len(reason) > 80 {
			reason = reason[:80]
		}
		out[toScore[r.Idx].ID] = ScoreEntry{Score: score, Reason: reason}
	}
	return out, nil
}

func buildScorePrompt(profile Profile, articles []Article) string {
	var b strings.Builder
	// The CLI runs as an agent that may load project context and try to be
	// conversational; the framing below keeps it acting as a pure JSON endpoint.
	b.WriteString("TASK: You are a JSON-only news-ranking function. This is a data task — do NOT ask questions, ")
	b.WriteString("do NOT add commentary, do NOT mention any project. Output only the JSON described at the end.\n\n")
	b.WriteString("Score each headline 0-100 for how likely THIS reader wants to read it. ")
	b.WriteString("100 = highly relevant to their taste, 0 = they would skip it. ")
	b.WriteString("Reward concrete, substantive Norwegian/Nordic and tech/gaming news; ")
	b.WriteString("penalise celebrity gossip, clickbait, sports trivia, and repetitive politics unless it matches their likes.\n\n")

	if len(profile.Likes) > 0 {
		b.WriteString("Headlines the reader LIKED (rank similar ones higher):\n")
		for _, t := range profile.Likes {
			b.WriteString("- " + t + "\n")
		}
		b.WriteString("\n")
	}
	if len(profile.Dislikes) > 0 {
		b.WriteString("Headlines the reader DISLIKED (rank similar ones lower):\n")
		for _, t := range profile.Dislikes {
			b.WriteString("- " + t + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("Articles to score:\n")
	for i, a := range articles {
		summary := a.Summary
		if len(summary) > 200 {
			summary = summary[:200]
		}
		fmt.Fprintf(&b, "%d. [%s] %s — %s\n", i, a.SourceName, a.Title, summary)
	}

	b.WriteString("\nRespond with ONLY a JSON array, no prose, no code fence. ")
	b.WriteString(`Each element: {"idx": <number>, "score": <0-100>, "reason": "<max 8 words>"}. `)
	b.WriteString("Include every article index exactly once.")
	return b.String()
}

// parseScoreJSON extracts the JSON array from the model output, tolerating
// surrounding prose or code fences.
func parseScoreJSON(raw string) ([]scoreResult, error) {
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end < 0 || end < start {
		return nil, fmt.Errorf("no JSON array in ranker output")
	}
	var results []scoreResult
	if err := json.Unmarshal([]byte(raw[start:end+1]), &results); err != nil {
		return nil, fmt.Errorf("parse ranker JSON: %w", err)
	}
	return results, nil
}
