package news

import (
	"encoding/xml"
	"html"
	"regexp"
	"strings"
	"time"
)

// rssFeed mirrors the subset of RSS 2.0 we consume. Field tags match on local
// element name, so namespaced elements like <vg:img> and <media:content> are
// captured by their local name ("img", "content").
type rssFeed struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string         `xml:"title"`
	Link        string         `xml:"link"`
	Description string         `xml:"description"`
	PubDate     string         `xml:"pubDate"`
	GUID        string         `xml:"guid"`
	Categories  []string       `xml:"category"`
	Enclosures  []rssEnclosure `xml:"enclosure"`
	Media       []rssMedia     `xml:"content"` // media:content
	VGImg       string         `xml:"img"`     // vg:img
	Image       string         `xml:"image"`   // VG <image> element
}

type rssEnclosure struct {
	URL  string `xml:"url,attr"`
	Type string `xml:"type,attr"`
}

type rssMedia struct {
	URL    string `xml:"url,attr"`
	Medium string `xml:"medium,attr"`
	Type   string `xml:"type,attr"`
}

var (
	tagRe       = regexp.MustCompile(`<[^>]*>`)
	wsRe        = regexp.MustCompile(`\s+`)
	dateLayouts = []string{
		time.RFC1123Z, // Wed, 03 Jun 2026 09:34:00 +0200 (gamer)
		time.RFC1123,  // Wed, 03 Jun 2026 10:08:10 GMT (VG/NRK/TV2/tek)
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 2 Jan 2006 15:04:05 MST",
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
	}
)

// parseFeed parses raw RSS bytes into Articles tagged with the given source.
func parseFeed(src Source, data []byte) ([]Article, error) {
	var feed rssFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, err
	}

	articles := make([]Article, 0, len(feed.Channel.Items))
	for _, it := range feed.Channel.Items {
		link := strings.TrimSpace(it.Link)
		guid := strings.TrimSpace(it.GUID)
		idSeed := link
		if idSeed == "" {
			idSeed = guid
		}
		if idSeed == "" {
			continue // nothing to key on
		}

		articles = append(articles, Article{
			ID:          ArticleID(idSeed),
			Source:      src.Key,
			SourceName:  src.Name,
			SourceColor: src.Color,
			Title:       cleanText(it.Title),
			URL:         link,
			Summary:     cleanText(it.Description),
			ImageURL:    pickImage(it),
			PublishedAt: parseDate(it.PubDate),
			Categories:  cleanCategories(it.Categories),
			Score:       -1,
		})
	}
	return articles, nil
}

// pickImage chooses the best available image URL from an item.
func pickImage(it rssItem) string {
	for _, m := range it.Media {
		if m.URL != "" && (m.Medium == "image" || strings.HasPrefix(m.Type, "image")) {
			return m.URL
		}
	}
	for _, m := range it.Media {
		if m.URL != "" {
			return m.URL
		}
	}
	for _, e := range it.Enclosures {
		if e.URL != "" && (strings.HasPrefix(e.Type, "image") || strings.HasPrefix(e.Type, "img")) {
			return e.URL
		}
	}
	if it.VGImg != "" {
		return strings.TrimSpace(it.VGImg)
	}
	if it.Image != "" && strings.HasPrefix(strings.TrimSpace(it.Image), "http") {
		return strings.TrimSpace(it.Image)
	}
	return ""
}

// cleanText strips HTML, unescapes entities, collapses whitespace and trims.
func cleanText(s string) string {
	s = html.UnescapeString(s)
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s) // entities sometimes survive a round of tag stripping
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// cleanCategories flattens category elements, splitting comma-joined lists
// (TV2 packs tags into one element) and de-duplicating.
func cleanCategories(cats []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, c := range cats {
		for _, part := range strings.Split(c, ",") {
			p := cleanText(part)
			if p == "" {
				continue
			}
			key := strings.ToLower(p)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, p)
		}
	}
	return out
}

// parseDate tries the known feed date layouts, returning zero time on failure.
func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
