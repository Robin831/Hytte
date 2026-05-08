package suggestions

import (
	"fmt"
	"strings"
)

// maxSourceFileChars caps the size of any one source file dropped into the prompt.
// Most React pages and Go handlers are well under this; larger files are
// truncated with a marker so the model can still see structure.
const maxSourceFileChars = 6000

// buildPagePrompt assembles a Claude prompt for generating page-improvement
// suggestions. The model is asked for a strict JSON array of exactly `target`
// objects, each with the fields {type, size, title, body}.
//
// target is the deficit between the per-page cap and the page's current
// pending-suggestion count, so the generator can request only what is needed
// to refill the page rather than always asking for three. Callers must pass
// target >= 1; the constant MaxPendingPerPage bounds the upper end.
//
// sourceFiles is a map of relative path → file contents. Callers truncate
// large files to keep the prompt within reasonable bounds; this function
// applies a safety cap regardless.
func buildPagePrompt(page Page, sourceFiles map[string]string, recentGitLog string, recentSuggestions []Suggestion, target int) string {
	var sb strings.Builder

	sb.WriteString("You are a senior software engineer reviewing one page of a personal web app called Hytte.\n")
	if target == 1 {
		sb.WriteString("Your job: propose one concrete, scoped improvement to that page.\n\n")
	} else {
		fmt.Fprintf(&sb, "Your job: propose %d concrete, scoped improvements to that page.\n\n", target)
	}

	sb.WriteString("## Page\n")
	fmt.Fprintf(&sb, "- Slug: %s\n", page.Slug)
	fmt.Fprintf(&sb, "- Title: %s\n", page.Title)
	fmt.Fprintf(&sb, "- Route: %s\n", page.Route)
	if page.Description != "" {
		fmt.Fprintf(&sb, "- Description: %s\n", page.Description)
	}
	if page.FeatureFlag != "" {
		fmt.Fprintf(&sb, "- Feature flag: %s\n", page.FeatureFlag)
	}
	sb.WriteString("\n")

	if len(sourceFiles) > 0 {
		sb.WriteString("## Source files (truncated)\n")
		for _, path := range page.SourceFiles {
			content, ok := sourceFiles[path]
			if !ok {
				continue
			}
			fmt.Fprintf(&sb, "### %s\n", path)
			sb.WriteString("```\n")
			sb.WriteString(truncate(content, maxSourceFileChars))
			sb.WriteString("\n```\n\n")
		}
	}

	if recentGitLog = strings.TrimSpace(recentGitLog); recentGitLog != "" {
		sb.WriteString("## Recent commits touching this page\n")
		sb.WriteString("```\n")
		sb.WriteString(recentGitLog)
		sb.WriteString("\n```\n\n")
	}

	if len(recentSuggestions) > 0 {
		sb.WriteString("## Recent suggestions for this page (avoid repeating these)\n")
		for _, s := range recentSuggestions {
			fmt.Fprintf(&sb, "- [%s/%s/%s] %s\n", s.Status, s.Type, s.Size, s.Title)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Output format\n\n")
	if target == 1 {
		sb.WriteString("Return ONLY a JSON array of exactly 1 object. Do not wrap in markdown fences.\n")
	} else {
		fmt.Fprintf(&sb, "Return ONLY a JSON array of exactly %d objects. Do not wrap in markdown fences.\n", target)
	}
	sb.WriteString(`Each object must have these fields and only these fields:

- "type": one of "addition", "bugfix", "improvement", "refactor"
- "size": one of "s" (under a day), "m" (one to three days), "l" (more than three days)
- "title": short imperative sentence, max 80 chars, no trailing period
- "body": 2 to 5 sentences explaining the problem, the proposed change, and the user-visible impact

`)
	if target > 1 {
		sb.WriteString("Each suggestion must have a unique type — all types must be different. Sizes may repeat freely: returning all of them the same size is fine if that reflects the page.\n\n")
	} else {
		sb.WriteString("Pick whichever type best fits the change.\n\n")
	}
	sb.WriteString("Example shape (do NOT copy these contents):\n")
	sb.WriteString(exampleShape(target))
	sb.WriteString("\n")

	return sb.String()
}

// exampleShape renders a JSON array example with exactly `target` objects so
// the prompt's example always matches the requested count. The first three
// slots use distinct types; additional slots cycle through the type list —
// only the count matters for the example, not the specific types used.
func exampleShape(target int) string {
	types := []string{"improvement", "addition", "bugfix", "refactor"}
	sizes := []string{"s", "m", "s", "m"}
	if target < 1 {
		target = 1
	}
	var sb strings.Builder
	sb.WriteString("[\n")
	for i := 0; i < target; i++ {
		t := types[i%len(types)]
		s := sizes[i%len(sizes)]
		fmt.Fprintf(&sb, "  {\"type\": %q, \"size\": %q, \"title\": \"...\", \"body\": \"...\"}", t, s)
		if i < target-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("]")
	return sb.String()
}

// truncate returns s clipped to at most max bytes, appending a marker when
// truncated. Operates on bytes; safe for the source files we feed it because
// React/Go source is ASCII-dominant and the marker only documents truncation.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... [truncated]"
}
