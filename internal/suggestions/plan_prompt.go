package suggestions

import (
	"fmt"
	"strings"
)

// maxPlanResponseChars caps the requested plan size in the prompt so Claude
// returns something the admin UI can render without truncation. The handler
// still trusts whatever comes back, but a hard size limit in the prompt keeps
// responses focused on essentials.
const maxPlanResponseChars = 3000

// buildPlanPrompt assembles a Claude prompt asking for a concrete
// implementation plan for one suggestion. The output is plain markdown — not
// JSON — and source files are intentionally NOT included: the planning step
// produces a high-level plan the operator (or another Claude session) will
// then execute, so feeding source bytes here would just bloat the prompt.
//
// page may be nil when the suggestion targets the synthetic "__new_page__"
// slug or when the original page slug is no longer in the registry; in both
// cases the prompt notes that page context is unavailable rather than
// fabricating it.
//
// feedback, if non-empty, is appended verbatim under a dedicated section so
// the admin can steer a re-plan without us re-interpreting their words.
func buildPlanPrompt(suggestion Suggestion, page *Page, feedback string) string {
	var sb strings.Builder

	sb.WriteString("You are a senior software engineer writing an implementation plan for a single Hytte suggestion.\n")
	sb.WriteString("The plan will be reviewed by the project owner and then handed to a coding agent that will execute it.\n\n")

	sb.WriteString("## Suggestion\n")
	fmt.Fprintf(&sb, "- Type: %s\n", suggestion.Type)
	fmt.Fprintf(&sb, "- Size: %s\n", suggestion.Size)
	fmt.Fprintf(&sb, "- Title: %s\n", suggestion.Title)
	if body := strings.TrimSpace(suggestion.Body); body != "" {
		sb.WriteString("- Body:\n")
		sb.WriteString(indentBlock(body, "  "))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	sb.WriteString("## Page context\n")
	if page == nil {
		if suggestion.PageSlug == NewPageSlug {
			sb.WriteString("This suggestion proposes a brand-new page (slug \"__new_page__\"). No existing page registry entry applies — design the plan as a greenfield addition.\n")
		} else {
			fmt.Fprintf(&sb, "Page slug %q is not in the registry; treat the page context as unavailable and base the plan on the suggestion body alone.\n", suggestion.PageSlug)
		}
	} else {
		fmt.Fprintf(&sb, "- Slug: %s\n", page.Slug)
		fmt.Fprintf(&sb, "- Title: %s\n", page.Title)
		if page.Route != "" {
			fmt.Fprintf(&sb, "- Route: %s\n", page.Route)
		}
		if page.Description != "" {
			fmt.Fprintf(&sb, "- Description: %s\n", page.Description)
		}
		if page.FeatureFlag != "" {
			fmt.Fprintf(&sb, "- Feature flag: %s\n", page.FeatureFlag)
		}
		if len(page.SourceFiles) > 0 {
			sb.WriteString("- Likely source files (for reference — do not assume contents):\n")
			for _, p := range page.SourceFiles {
				fmt.Fprintf(&sb, "  - %s\n", p)
			}
		}
	}
	sb.WriteString("\n")

	if trimmed := strings.TrimSpace(feedback); trimmed != "" {
		sb.WriteString("## User feedback\n")
		sb.WriteString("Treat the following feedback as guidance from the project owner. It overrides your default judgement where they conflict.\n\n")
		sb.WriteString(trimmed)
		sb.WriteString("\n\n")
	}

	fmt.Fprintf(&sb, `## Output format

Return plain GitHub-flavored markdown — NOT JSON, NOT wrapped in a code fence. Keep the entire response under %d characters; be concise and concrete. Use these sections in order:

### Scope
One short paragraph describing what this plan covers.

### Files to touch
A bulleted list of the specific files (or new files) you expect to add or modify. Mark new files with "(new)".

### Acceptance criteria
A bulleted list of observable conditions that must all be true for the work to be considered done. Each bullet should be testable from the user's or maintainer's perspective.

### Non-goals
A bulleted list of things this plan deliberately does NOT cover, to prevent scope creep.

Do not include source code, large diff blocks, or speculation about contents you have not been shown.
`, maxPlanResponseChars)

	return sb.String()
}

// indentBlock prefixes every line of s with prefix. Used to nest a multi-line
// suggestion body under a bullet without breaking markdown rendering.
func indentBlock(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
