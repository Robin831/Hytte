package suggestions

import (
	"strings"
	"testing"
)

func TestBuildPlanPromptIncludesSuggestionFields(t *testing.T) {
	s := Suggestion{
		PageSlug: "weather",
		Type:     TypeImprovement,
		Size:     SizeM,
		Title:    "Cache the forecast",
		Body:     "Add a 10-minute in-memory cache.",
	}
	page := &Page{
		Slug:        "weather",
		Title:       "Weather",
		Route:       "/weather",
		Description: "Multi-day forecast.",
		FeatureFlag: "weather",
		SourceFiles: []string{"web/src/pages/Weather.tsx"},
	}

	prompt := buildPlanPrompt(s, page, "")

	for _, want := range []string{
		"Cache the forecast",
		"Add a 10-minute in-memory cache.",
		"Slug: weather",
		"Route: /weather",
		"Feature flag: weather",
		"web/src/pages/Weather.tsx",
		"### Scope",
		"### Files to touch",
		"### Acceptance criteria",
		"### Non-goals",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected prompt to contain %q, missing.\nFull prompt:\n%s", want, prompt)
		}
	}

	if strings.Contains(prompt, "## User feedback") {
		t.Errorf("did not expect a User feedback section when feedback is empty")
	}
	if strings.Contains(prompt, "JSON") && !strings.Contains(prompt, "NOT JSON") {
		t.Errorf("plan prompt should explicitly forbid JSON output")
	}
}

func TestBuildPlanPromptHandlesNilPageForNewPageSlug(t *testing.T) {
	s := Suggestion{
		PageSlug: NewPageSlug,
		Type:     TypeNewPage,
		Size:     SizeL,
		Title:    "Add expense tracker",
		Body:     "A new page that tracks expenses.",
	}

	prompt := buildPlanPrompt(s, nil, "")

	if !strings.Contains(prompt, "__new_page__") {
		t.Errorf("expected greenfield notice referencing __new_page__, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "greenfield") && !strings.Contains(prompt, "brand-new page") {
		t.Errorf("expected the prompt to indicate this is a brand-new page, got:\n%s", prompt)
	}
}

func TestBuildPlanPromptHandlesNilPageForUnknownSlug(t *testing.T) {
	s := Suggestion{
		PageSlug: "deprecated-page",
		Type:     TypeImprovement,
		Size:     SizeS,
		Title:    "Tweak something",
		Body:     "Detail.",
	}

	prompt := buildPlanPrompt(s, nil, "")

	if !strings.Contains(prompt, "deprecated-page") {
		t.Errorf("expected the unknown slug to appear in the prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "not in the registry") {
		t.Errorf("expected the prompt to note the page is missing from the registry, got:\n%s", prompt)
	}
}

func TestBuildPlanPromptIncludesFeedbackVerbatim(t *testing.T) {
	s := Suggestion{
		PageSlug: "weather",
		Type:     TypeImprovement,
		Size:     SizeS,
		Title:    "Cache the forecast",
		Body:     "Add caching.",
	}
	page := &Page{Slug: "weather", Title: "Weather"}
	feedback := "Please use Redis instead of an in-memory map; we already run Redis for sessions."

	prompt := buildPlanPrompt(s, page, feedback)

	if !strings.Contains(prompt, "## User feedback") {
		t.Errorf("expected a User feedback section, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, feedback) {
		t.Errorf("expected feedback %q to appear verbatim in prompt, got:\n%s", feedback, prompt)
	}
}

func TestBuildPlanPromptIgnoresWhitespaceOnlyFeedback(t *testing.T) {
	s := Suggestion{PageSlug: "weather", Type: TypeImprovement, Size: SizeS, Title: "T", Body: "B"}
	page := &Page{Slug: "weather", Title: "Weather"}

	prompt := buildPlanPrompt(s, page, "   \n\t  ")

	if strings.Contains(prompt, "## User feedback") {
		t.Errorf("whitespace-only feedback should not produce a feedback section, got:\n%s", prompt)
	}
}
