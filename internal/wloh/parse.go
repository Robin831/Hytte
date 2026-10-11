// Package wloh reads and writes Wordfeud League of Honour
// (wordfeud.aasmul.net), the fan-run league site. The site has no API: it is
// an ASP.NET WebForms app, so groups are scraped from HTML and results are
// posted by replaying its form.
package wloh

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Group is one league group (a "serie"): eight or so players who all play
// each other during a two-week season.
type Group struct {
	SerieID   int64      `json:"serie_id"`
	LeagueID  int        `json:"league_id"`
	League    string     `json:"league"`
	Season    int        `json:"season"`
	Division  int        `json:"division"`
	GroupNo   int        `json:"group"`
	Deadline  time.Time  `json:"deadline"`
	URL       string     `json:"url"`
	Standings []Standing `json:"standings"`
	Matches   []Match    `json:"matches"`
}

// Standing is one row of the group table.
type Standing struct {
	Place    int    `json:"place"`
	Name     string `json:"name"`
	Played   int    `json:"played"`
	Won      int    `json:"won"`
	Drawn    int    `json:"drawn"`
	Lost     int    `json:"lost"`
	ScoreFor int    `json:"score_for"`
	ScoreAg  int    `json:"score_against"`
	Margin   int    `json:"margin"`
	Points   int    `json:"points"`
	From     string `json:"from,omitempty"`
}

// Match is a registered result. The site lists every pairing twice, once
// from each player's side; Group.Matches keeps both.
type Match struct {
	Home      string `json:"home"`
	Away      string `json:"away"`
	HomeScore int    `json:"home_score"`
	AwayScore int    `json:"away_score"`
}

// Player returns the standings name matching username case-insensitively,
// or "" when the player is not in the group.
func (g *Group) Player(username string) string {
	for _, s := range g.Standings {
		if strings.EqualFold(s.Name, username) {
			return s.Name
		}
	}
	return ""
}

// Result returns the registered result between home and away, seen from
// home's side.
func (g *Group) Result(home, away string) (homeScore, awayScore int, ok bool) {
	for _, m := range g.Matches {
		if strings.EqualFold(m.Home, home) && strings.EqualFold(m.Away, away) {
			return m.HomeScore, m.AwayScore, true
		}
		if strings.EqualFold(m.Home, away) && strings.EqualFold(m.Away, home) {
			return m.AwayScore, m.HomeScore, true
		}
	}
	return 0, 0, false
}

const (
	idLeague     = "MainContent_DropDownList4Spraak"
	idSeason     = "MainContent_DropDownList5Sesong"
	idDivision   = "MainContent_DropDownList6Divisjon"
	idSerie      = "MainContent_DropDownList3Avdeling"
	idTable      = "MainContent_DivTabell"
	idDeadline   = "MainContent_Label58Deadline"
	idLoginName  = "HeadLoginView_HeadLoginName"
	idHomeSelect = "MainContent_DropDownList2Hjemmespiller"
	idAwaySelect = "MainContent_DropDownList1Bortespiller"
	idFormNotice = "MainContent_Label55"
	idPageNotice = "MainContent_Label2"

	matchPlayerPrefix = "MainContent_ListView1_spelar_"
	matchResultPrefix = "MainContent_ListView1_resultat_"
)

// page is a parsed WLoH page with its elements indexed by id.
type page struct {
	root *html.Node
	byID map[string]*html.Node
}

func parsePage(body string) (*page, error) {
	root, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("wloh: parse html: %w", err)
	}
	p := &page{root: root, byID: map[string]*html.Node{}}
	walk(root, func(n *html.Node) {
		if id := attr(n, "id"); id != "" {
			if _, dup := p.byID[id]; !dup {
				p.byID[id] = n
			}
		}
	})
	return p, nil
}

func walk(n *html.Node, fn func(*html.Node)) {
	if n.Type == html.ElementNode {
		fn(n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func nodeText(n *html.Node) string {
	if n == nil {
		return ""
	}
	var sb strings.Builder
	var rec func(*html.Node)
	rec = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			rec(c)
		}
	}
	rec(n)
	return strings.Join(strings.Fields(sb.String()), " ")
}

func (p *page) text(id string) string { return nodeText(p.byID[id]) }

// option is one <option> of a <select>.
type option struct {
	value, label string
	selected     bool
}

func (p *page) options(id string) []option {
	sel := p.byID[id]
	if sel == nil {
		return nil
	}
	var out []option
	walk(sel, func(n *html.Node) {
		if n.Data == "option" {
			out = append(out, option{value: attr(n, "value"), label: nodeText(n), selected: hasAttr(n, "selected")})
		}
	})
	return out
}

// selected returns the chosen option of a <select>: the one marked selected,
// else the first, as a browser would submit it.
func (p *page) selected(id string) (option, bool) {
	opts := p.options(id)
	for _, o := range opts {
		if o.selected {
			return o, true
		}
	}
	if len(opts) > 0 {
		return opts[0], true
	}
	return option{}, false
}

// formFields returns what a browser would submit for the page's form with no
// button pressed: hidden and text inputs, textareas and selects.
func (p *page) formFields() map[string]string {
	fields := map[string]string{}
	walk(p.root, func(n *html.Node) {
		name := attr(n, "name")
		if name == "" {
			return
		}
		switch n.Data {
		case "input":
			switch strings.ToLower(attr(n, "type")) {
			case "hidden", "text", "email", "":
				fields[name] = attr(n, "value")
			}
		case "textarea":
			fields[name] = nodeText(n)
		case "select":
			if o, ok := p.selected(attr(n, "id")); ok {
				fields[name] = o.value
			}
		}
	})
	return fields
}

// formAction returns the action of the page's form, "" when there is none.
func (p *page) formAction() string {
	action := ""
	walk(p.root, func(n *html.Node) {
		if n.Data == "form" && action == "" {
			action = attr(n, "action")
		}
	})
	return action
}

var (
	deadlineRe = regexp.MustCompile(`(\d{1,2})/(\d{1,2}) (\d{4}) (\d{1,2}):(\d{2})`)
	scoreRe    = regexp.MustCompile(`^(-?\d+)\s*[–-]\s*(-?\d+)$`)
)

// zone is the WLoH clock: Central European Time including summer time.
func zone() *time.Location {
	if loc, err := time.LoadLocation("Europe/Oslo"); err == nil {
		return loc
	}
	return time.FixedZone("CET", 3600)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// group extracts the group shown on a tables page.
func (p *page) group() (*Group, error) {
	g := &Group{}
	serie, ok := p.selected(idSerie)
	if !ok {
		return nil, fmt.Errorf("wloh: no group selector on page")
	}
	g.SerieID, _ = strconv.ParseInt(serie.value, 10, 64)
	g.GroupNo = atoi(serie.label)
	if g.SerieID <= 0 {
		return nil, fmt.Errorf("wloh: unexpected group id %q", serie.value)
	}
	if o, ok := p.selected(idLeague); ok {
		g.LeagueID, g.League = atoi(o.value), o.label
	}
	if o, ok := p.selected(idSeason); ok {
		g.Season = atoi(o.value)
	}
	if o, ok := p.selected(idDivision); ok {
		g.Division = atoi(o.value)
	}
	if m := deadlineRe.FindStringSubmatch(p.text(idDeadline)); m != nil {
		g.Deadline = time.Date(atoi(m[3]), time.Month(atoi(m[2])), atoi(m[1]), atoi(m[4]), atoi(m[5]), 0, 0, zone())
	}

	if table := p.byID[idTable]; table != nil {
		walk(table, func(tr *html.Node) {
			if tr.Data != "tr" {
				return
			}
			var cells []string
			for c := tr.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && c.Data == "td" {
					cells = append(cells, nodeText(c))
				}
			}
			if len(cells) < 9 || cells[1] == "" {
				return
			}
			s := Standing{
				Place:  atoi(cells[0]),
				Name:   cells[1],
				Played: atoi(cells[2]),
				Won:    atoi(cells[3]),
				Drawn:  atoi(cells[4]),
				Lost:   atoi(cells[5]),
				Margin: atoi(cells[7]),
				Points: atoi(cells[8]),
			}
			if m := scoreRe.FindStringSubmatch(cells[6]); m != nil {
				s.ScoreFor, s.ScoreAg = atoi(m[1]), atoi(m[2])
			}
			if len(cells) > 9 {
				s.From = cells[9]
			}
			g.Standings = append(g.Standings, s)
		})
	}
	if len(g.Standings) == 0 {
		return nil, fmt.Errorf("wloh: no standings on page")
	}

	// Rows are numbered from 0 without gaps; stop at the first missing one.
	for i := 0; ; i++ {
		idx := strconv.Itoa(i)
		n := p.byID[matchPlayerPrefix+idx]
		if n == nil {
			break
		}
		sm := scoreRe.FindStringSubmatch(p.text(matchResultPrefix + idx))
		if sm == nil {
			continue
		}
		home, away, ok := g.splitPairing(nodeText(n))
		if !ok {
			continue
		}
		g.Matches = append(g.Matches, Match{Home: home, Away: away, HomeScore: atoi(sm[1]), AwayScore: atoi(sm[2])})
	}
	return g, nil
}

// splitPairing splits "home – away" using the known player names, so a name
// containing a dash can't split in the wrong place.
func (g *Group) splitPairing(s string) (home, away string, ok bool) {
	for _, h := range g.Standings {
		rest, found := strings.CutPrefix(s, h.Name)
		if !found {
			continue
		}
		rest = strings.TrimSpace(rest)
		rest, found = strings.CutPrefix(rest, "–")
		if !found {
			continue
		}
		rest = strings.TrimSpace(rest)
		if name := g.Player(rest); name != "" && name != h.Name {
			return h.Name, name, true
		}
	}
	return "", "", false
}
