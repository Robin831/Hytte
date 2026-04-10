package homework

import (
	"strconv"
	"strings"
)

// Subject keywords for classification.
var (
	mathKeywords = []string{
		"equation", "solve", "calculate", "algebra", "geometry", "fraction",
		"multiply", "divide", "subtract", "add", "derivative", "integral",
		"graph", "number", "math", "arithmetic", "trigonometry", "percentage",
		"ratio", "exponent",
	}
	writingKeywords = []string{
		"essay", "write", "paragraph", "thesis", "draft", "composition",
		"grammar", "punctuation", "narrative", "persuasive", "argument",
		"outline", "topic sentence", "writing",
	}
	readingKeywords = []string{
		"read", "book", "chapter", "character", "plot", "theme",
		"literature", "story", "author", "comprehension", "vocabulary",
		"passage", "novel", "poem",
	}
)

// DetectSubject scans message text for subject indicators and returns the
// best-matching subject: "math", "writing", "reading", or "general".
func DetectSubject(messageText string) string {
	lower := strings.ToLower(messageText)

	mathCount := countKeywords(lower, mathKeywords)
	writingCount := countKeywords(lower, writingKeywords)
	readingCount := countKeywords(lower, readingKeywords)

	if mathCount == 0 && writingCount == 0 && readingCount == 0 {
		return "general"
	}

	best := "general"
	bestCount := 0

	if mathCount > bestCount {
		best = "math"
		bestCount = mathCount
	}
	if writingCount > bestCount {
		best = "writing"
		bestCount = writingCount
	}
	if readingCount > bestCount {
		best = "reading"
	}

	return best
}

func countKeywords(text string, keywords []string) int {
	count := 0
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			count++
		}
	}
	return count
}

// BuildSystemPrompt assembles the full system prompt for a homework tutoring
// session. It composes tutor framing, age calibration, subject-specific rules,
// anti-cheating guardrails, and help-level enforcement.
func BuildSystemPrompt(profile HomeworkProfile, helpLevel HelpLevel, detectedSubject string) string {
	var b strings.Builder

	// Section 1: Tutor framing preamble
	b.WriteString("You are a friendly, patient homework tutor. Your goal is to help the student understand concepts and develop problem-solving skills, not to give them answers. Encourage curiosity and celebrate effort.\n\n")

	// Section 2: Age/grade calibration
	b.WriteString(ageCalibration(profile))

	// Section 3: Subject-specific pedagogical rules
	b.WriteString(subjectRules(detectedSubject))

	// Section 4: Anti-cheating guardrails
	b.WriteString("IMPORTANT RULES:\n")
	b.WriteString("- Never provide complete answers directly.\n")
	b.WriteString("- Do not do the student's work for them.\n")
	b.WriteString("- If the student asks you to just give the answer, gently redirect them to think through the problem.\n")
	b.WriteString("- Do not generate entire essays, solutions, or assignments.\n\n")

	// Section 5: Help-level enforcement
	b.WriteString(helpLevelRules(helpLevel))

	return b.String()
}

func ageCalibration(profile HomeworkProfile) string {
	age := profile.Age
	grade := parseGrade(profile.GradeLevel)

	if age <= 8 || grade <= 3 {
		return "Use simple, clear language appropriate for a young child. Keep sentences short. Use everyday examples and be very encouraging.\n\n"
	}
	if age <= 13 || grade <= 8 {
		return "Use moderate vocabulary appropriate for a middle-school student. You can introduce subject-specific terms but explain them when first used.\n\n"
	}
	return "Use advanced vocabulary appropriate for a high-school student. You can use subject-specific terminology and expect more independent reasoning.\n\n"
}

// parseGrade extracts a numeric grade from strings like "3", "5th", "grade 7".
func parseGrade(gradeLevel string) int {
	cleaned := strings.ToLower(gradeLevel)
	cleaned = strings.TrimPrefix(cleaned, "grade ")
	cleaned = strings.TrimPrefix(cleaned, "grade")
	cleaned = strings.TrimSuffix(cleaned, "th")
	cleaned = strings.TrimSuffix(cleaned, "st")
	cleaned = strings.TrimSuffix(cleaned, "nd")
	cleaned = strings.TrimSuffix(cleaned, "rd")
	cleaned = strings.TrimSpace(cleaned)

	n, err := strconv.Atoi(cleaned)
	if err != nil {
		return 0
	}
	return n
}

func subjectRules(subject string) string {
	switch subject {
	case "math":
		return "MATH TUTORING RULES:\n" +
			"- Always enforce step-by-step reasoning. Do not skip steps.\n" +
			"- Ask the student to show their work before offering help.\n" +
			"- When correcting errors, point to the specific step where the mistake occurred.\n" +
			"- Use visual representations (number lines, diagrams) when helpful.\n\n"
	case "writing":
		return "WRITING TUTORING RULES:\n" +
			"- Never write essays or full paragraphs for the student.\n" +
			"- Redirect to brainstorming, outlining, and revision techniques.\n" +
			"- Ask guiding questions to help the student develop their own ideas.\n" +
			"- Focus on structure, clarity, and the writing process.\n\n"
	case "reading":
		return "READING TUTORING RULES:\n" +
			"- Generate discussion questions to deepen comprehension.\n" +
			"- Encourage the student to support answers with text evidence.\n" +
			"- Help with vocabulary by providing context clues before definitions.\n" +
			"- Ask the student to make predictions and connections.\n\n"
	default:
		return ""
	}
}

func helpLevelRules(level HelpLevel) string {
	switch level {
	case HelpLevelHint:
		return "HELP LEVEL - HINTS ONLY:\n" +
			"Provide hints only. Do not reveal the answer. Give just enough direction for the student to take the next step on their own.\n"
	case HelpLevelExplain:
		return "HELP LEVEL - EXPLAIN:\n" +
			"Explain the relevant concepts and principles. Help the student understand the 'why' behind the problem, but let them apply it themselves.\n"
	case HelpLevelWalkthrough:
		return "HELP LEVEL - GUIDED WALKTHROUGH:\n" +
			"Provide a guided walkthrough, explaining each step but letting the student attempt each part before moving on.\n"
	case HelpLevelAnswer:
		return "HELP LEVEL - EXPLAIN SOLUTION:\n" +
			"Explain the full solution step-by-step so the student can learn from it. Still focus on teaching, not just giving the answer.\n"
	default:
		return "HELP LEVEL - HINTS ONLY:\n" +
			"Provide hints only. Do not reveal the answer. Give just enough direction for the student to take the next step on their own.\n"
	}
}
