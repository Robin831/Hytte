package news

import (
	"strings"
	"unicode"
)

// paywallMarkers are teaser phrases that indicate a locked/abonnement article.
// RSS feeds don't expose a reliable per-item paywall flag, so this is
// best-effort and surfaced transparently in the "why filtered" drawer.
var paywallMarkers = []string{
	"les hele saken", "kun for abonnenter", "bli abonnent", "få full tilgang",
	"denne saken er forbeholdt", "for abonnenter", "abonnement",
}

// applyFilters splits articles into the ones to show and the ones hidden by a
// hard filter (with the reason, for the audit drawer).
func applyFilters(articles []Article, s Settings) (visible []Article, hidden []HiddenArticle) {
	keywords := normalizeList(s.BlockKeywords)
	categories := map[string]bool{}
	for _, c := range s.BlockCategories {
		categories[strings.ToLower(strings.TrimSpace(c))] = true
	}

	for _, a := range articles {
		if reason := matchReason(a, keywords, categories, s.HidePaywalled); reason != "" {
			hidden = append(hidden, HiddenArticle{Article: a, Reason: reason})
			continue
		}
		visible = append(visible, a)
	}
	return visible, hidden
}

func matchReason(a Article, keywords []string, categories map[string]bool, hidePaywall bool) string {
	if hidePaywall && isPaywalled(a) {
		return "paywall"
	}
	for _, c := range a.Categories {
		if categories[strings.ToLower(c)] {
			return "category:" + c
		}
	}
	hay := strings.ToLower(a.Title + " " + a.Summary + " " + strings.Join(a.Categories, " "))
	for _, kw := range keywords {
		if containsWord(hay, kw) {
			return "keyword:" + kw
		}
	}
	return ""
}

func isPaywalled(a Article) bool {
	if strings.HasPrefix(strings.TrimSpace(a.Title), "+") {
		return true
	}
	low := strings.ToLower(a.Summary)
	for _, m := range paywallMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

func normalizeList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// containsWord reports whether needle appears in haystack bounded by
// non-letters/digits (so "iran" does not match "miranda"). Both must be
// lowercase. Works for multi-word phrases and Norwegian letters (uses
// unicode.IsLetter, unlike regexp \b which is ASCII-only).
func containsWord(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	hr := []rune(haystack)
	nr := []rune(needle)
	for i := 0; i+len(nr) <= len(hr); i++ {
		if !runesEqual(hr[i:i+len(nr)], nr) {
			continue
		}
		beforeOK := i == 0 || !isWordRune(hr[i-1])
		afterOK := i+len(nr) >= len(hr) || !isWordRune(hr[i+len(nr)])
		if beforeOK && afterOK {
			return true
		}
	}
	return false
}

func runesEqual(a, b []rune) bool {
	for i := range b {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// dedupe collapses near-duplicate stories that appear in more than one source.
// The first (newest, since input is sorted) occurrence is kept and later
// duplicates are recorded as AlsoIn references. Same-source items are never
// merged (they're genuinely different articles).
func dedupe(articles []Article) []Article {
	type kept struct {
		idx    int
		tokens map[string]bool
	}
	var keptList []kept
	out := make([]Article, 0, len(articles))

	for _, a := range articles {
		toks := titleTokens(a.Title)
		merged := false
		for _, k := range keptList {
			if out[k.idx].Source == a.Source {
				continue
			}
			if jaccard(k.tokens, toks) >= 0.6 {
				out[k.idx].AlsoIn = append(out[k.idx].AlsoIn, SourceRef{
					Source: a.Source, SourceName: a.SourceName, URL: a.URL,
				})
				merged = true
				break
			}
		}
		if merged {
			continue
		}
		out = append(out, a)
		keptList = append(keptList, kept{idx: len(out) - 1, tokens: toks})
	}
	return out
}

// stopwords are short Norwegian/English words ignored when comparing titles.
var stopwords = map[string]bool{
	"i": true, "og": true, "på": true, "av": true, "til": true, "for": true,
	"med": true, "en": true, "et": true, "er": true, "som": true, "den": true,
	"det": true, "de": true, "the": true, "a": true, "an": true, "of": true,
	"to": true, "in": true, "on": true, "and": true, "har": true, "ble": true,
}

func titleTokens(title string) map[string]bool {
	toks := map[string]bool{}
	for _, f := range strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(f) < 3 || stopwords[f] {
			continue
		}
		toks[f] = true
	}
	return toks
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
