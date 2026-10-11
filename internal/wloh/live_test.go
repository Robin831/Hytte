package wloh

import (
	"context"
	"os"
	"testing"
)

// TestLiveFindGroup reads the real site. It is skipped unless WLOH_LIVE_USER
// is set; run it when the scraper stops working to see what changed.
func TestLiveFindGroup(t *testing.T) {
	user := os.Getenv("WLOH_LIVE_USER")
	if user == "" {
		t.Skip("set WLOH_LIVE_USER to run against the live site")
	}
	c := NewClient()
	g, err := c.FindGroup(context.Background(), user)
	if err != nil {
		t.Fatalf("FindGroup: %v", err)
	}
	t.Logf("%s season %d division %d group %d (serie %d), deadline %s, %d players, %d results",
		g.League, g.Season, g.Division, g.GroupNo, g.SerieID, g.Deadline, len(g.Standings), len(g.Matches))
	again, err := c.GetGroup(context.Background(), g.SerieID)
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if again.SerieID != g.SerieID || len(again.Standings) != len(g.Standings) || again.Deadline.IsZero() {
		t.Errorf("GetGroup disagrees with FindGroup: %+v vs %+v", again, g)
	}
	for _, s := range g.Standings {
		t.Logf("%d %-16s played %d  %d-%d  margin %d  points %d", s.Place, s.Name, s.Played, s.ScoreFor, s.ScoreAg, s.Margin, s.Points)
	}
	for _, m := range g.Matches {
		t.Logf("%s %d - %d %s", m.Home, m.HomeScore, m.AwayScore, m.Away)
	}
}
