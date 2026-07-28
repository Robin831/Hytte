package offers

import (
	"sort"
	"strings"
	"unicode"
)

// Rank annotates offers with discount percentage and watchlist matches, then
// orders them for display: watchlist matches first, then by discount (best
// first); offers without a known before-price sort after discounted ones,
// alphabetically. Matching is rune-aware and compound-friendly: Norwegian
// compounds put the product last (helMELK, tøyVASKEMIDDEL), so the keyword
// must end at a word boundary but may start mid-word — "melk" matches
// "HELMELK" and "MELK 1L" but not "melkesjokolade". Multi-word keywords match
// as phrases.
func Rank(offers []Offer, keywords []string) []RankedOffer {
	lowered := make([]string, len(keywords))
	for i, k := range keywords {
		lowered[i] = strings.ToLower(strings.TrimSpace(k))
	}

	ranked := make([]RankedOffer, 0, len(offers))
	for _, o := range offers {
		r := RankedOffer{Offer: o}
		if o.PrePrice > o.Price && o.PrePrice > 0 {
			r.DiscountPct = int((o.PrePrice-o.Price)/o.PrePrice*100 + 0.5)
		}
		haystack := strings.ToLower(o.Heading + " " + o.Description)
		for i, k := range lowered {
			if k == "" {
				continue
			}
			if containsCompound(haystack, k) {
				r.MatchedKeywords = append(r.MatchedKeywords, keywords[i])
			}
		}
		ranked = append(ranked, r)
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if (len(a.MatchedKeywords) > 0) != (len(b.MatchedKeywords) > 0) {
			return len(a.MatchedKeywords) > 0
		}
		if a.DiscountPct != b.DiscountPct {
			return a.DiscountPct > b.DiscountPct
		}
		return a.Heading < b.Heading
	})
	return ranked
}

// containsCompound reports whether needle occurs in haystack ending at a word
// boundary; the start may be mid-word so Norwegian compound heads match
// (helMELK). Rune-aware (adapted from internal/news/filter.go's containsWord,
// which requires both boundaries) so Norwegian letters work, unlike regexp's
// ASCII \b.
func containsCompound(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	hr := []rune(haystack)
	nr := []rune(needle)
	for i := 0; i+len(nr) <= len(hr); i++ {
		if !runesEqual(hr[i:i+len(nr)], nr) {
			continue
		}
		if i+len(nr) >= len(hr) || !isWordRune(hr[i+len(nr)]) {
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

func equalFold(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
