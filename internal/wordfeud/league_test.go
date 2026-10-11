package wordfeud

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/wloh"
)

var leagueDeadline = time.Date(2026, 10, 23, 23, 59, 0, 0, time.UTC)

// inSeason is a Unix time a few hours into the test season.
var inSeason = leagueDeadline.Add(-seasonLength).Add(6 * time.Hour).Unix()

func testGroup() *wloh.Group {
	return &wloh.Group{
		SerieID: 965530, LeagueID: 1, League: "Wordfeud Norsk", Season: 398, Division: 5, GroupNo: 11,
		Deadline: leagueDeadline,
		Standings: []wloh.Standing{
			{Place: 1, Name: "tjobias"}, {Place: 2, Name: "ave62"}, {Place: 3, Name: "Illkl"},
			{Place: 4, Name: "jenscb"}, {Place: 5, Name: "Luremus"}, {Place: 6, Name: "1roxy1"},
		},
		Matches: []wloh.Match{{Home: "Luremus", Away: "1roxy1", HomeScore: 500, AwayScore: 400}},
	}
}

func statusOf(matches []LeagueMatch, opponent string) LeagueMatch {
	for _, m := range matches {
		if m.Opponent == opponent {
			return m
		}
	}
	return LeagueMatch{}
}

func TestBuildLeagueMatches(t *testing.T) {
	games := &GamesResult{
		Active: []GameSummary{
			{ID: 1, Opponent: "TJOBIAS", Scores: [2]int{120, 90}, IsMyTurn: true, IsRunning: true, CreatedAt: inSeason, Ruleset: RulesetNorwegian},
			// The only game against jenscb this season counts as the league
			// match even in another language, so no second invite goes out.
			{ID: 2, Opponent: "jenscb", IsRunning: true, CreatedAt: inSeason, Ruleset: RulesetEnglishIntl},
			{ID: 3, Opponent: "stranger", IsRunning: true, CreatedAt: inSeason, Ruleset: RulesetNorwegian},
		},
		Finished: []GameSummary{
			{ID: 4, Opponent: "ave62", Scores: [2]int{480, 469}, CreatedAt: inSeason, Ruleset: RulesetNorwegian, LastMove: MoveInfo{MoveType: "move"}},
			// A finished game from an earlier season must not be picked up.
			{ID: 5, Opponent: "Illkl", Scores: [2]int{300, 310}, CreatedAt: inSeason - int64(30*24*3600), Ruleset: RulesetNorwegian},
			{ID: 6, Opponent: "1roxy1", Scores: [2]int{500, 400}, CreatedAt: inSeason, Ruleset: RulesetNorwegian},
		},
	}
	invites := &Invites{Sent: []string{"illkl"}, Received: []string{"JENSCB"}}
	matches := buildLeagueMatches(testGroup(), "luremus", games, invites)

	if len(matches) != 5 {
		t.Fatalf("got %d matches, want 5 (everyone but me): %+v", len(matches), matches)
	}
	if m := statusOf(matches, "tjobias"); m.Status != MatchPlaying || m.GameID != 1 || !m.IsMyTurn || m.MyScore != 120 {
		t.Errorf("tjobias = %+v", m)
	}
	if m := statusOf(matches, "ave62"); m.Status != MatchToReport || m.GameID != 4 || m.MyScore != 480 || m.OppScore != 469 {
		t.Errorf("ave62 = %+v", m)
	}
	if m := statusOf(matches, "Illkl"); m.Status != MatchInviteSent || m.GameID != 0 {
		t.Errorf("Illkl = %+v", m)
	}
	if m := statusOf(matches, "jenscb"); m.Status != MatchPlaying || m.GameID != 2 {
		t.Errorf("jenscb = %+v", m)
	}
	if m := statusOf(matches, "1roxy1"); m.Status != MatchReported || m.MyScore != 500 || m.OppScore != 400 {
		t.Errorf("1roxy1 = %+v", m)
	}
}

func TestBuildLeagueMatchesWithoutWordfeud(t *testing.T) {
	matches := buildLeagueMatches(testGroup(), "Luremus", nil, nil)
	if m := statusOf(matches, "tjobias"); m.Status != MatchNotStarted {
		t.Errorf("tjobias = %+v", m)
	}
	if m := statusOf(matches, "1roxy1"); m.Status != MatchReported {
		t.Errorf("1roxy1 = %+v", m)
	}
}

func TestLeagueGamePrefersEarliestAndHandlesMissingStart(t *testing.T) {
	g := testGroup()
	games := &GamesResult{Active: []GameSummary{
		{ID: 1, Opponent: "tjobias", IsRunning: true, CreatedAt: inSeason + 5000, Ruleset: -1},
		{ID: 2, Opponent: "tjobias", IsRunning: true, CreatedAt: inSeason, Ruleset: -1},
	}}
	if got := leagueGame(g, "tjobias", games); got == nil || got.ID != 2 {
		t.Errorf("picked %+v, want the earliest game of the season", got)
	}
	// No start time: a running game counts, an old finished one does not.
	games = &GamesResult{
		Active:   []GameSummary{{ID: 3, Opponent: "ave62", IsRunning: true, Ruleset: -1}},
		Finished: []GameSummary{{ID: 4, Opponent: "Illkl", EndedAt: inSeason - int64(20*24*3600), Ruleset: -1}},
	}
	if got := leagueGame(g, "ave62", games); got == nil || got.ID != 3 {
		t.Errorf("running game without start time not matched: %+v", got)
	}
	if got := leagueGame(g, "Illkl", games); got != nil {
		t.Errorf("old finished game matched: %+v", got)
	}
}

func TestLeagueGameFallsBackWhenRulesetDiffers(t *testing.T) {
	g := testGroup()
	games := &GamesResult{Active: []GameSummary{
		{ID: 1, Opponent: "tjobias", IsRunning: true, CreatedAt: inSeason, Ruleset: 42},
		{ID: 2, Opponent: "ave62", IsRunning: true, CreatedAt: inSeason, Ruleset: 42},
		{ID: 3, Opponent: "ave62", IsRunning: true, CreatedAt: inSeason + 10, Ruleset: RulesetNorwegian},
	}}
	// The only game against tjobias is the league game, whatever its ruleset id.
	if got := leagueGame(g, "tjobias", games); got == nil || got.ID != 1 {
		t.Errorf("tjobias: picked %+v, want game 1", got)
	}
	// With a choice, the game in the league's language wins.
	if got := leagueGame(g, "ave62", games); got == nil || got.ID != 3 {
		t.Errorf("ave62: picked %+v, want game 3", got)
	}
}

func TestInviteRuleset(t *testing.T) {
	g := testGroup()
	if r, ok := inviteRuleset(g, "luremus", nil); !ok || r != RulesetNorwegian {
		t.Errorf("no games: got %d %v, want the league's ruleset", r, ok)
	}
	games := &GamesResult{Active: []GameSummary{
		{ID: 1, Opponent: "tjobias", IsRunning: true, CreatedAt: inSeason, Ruleset: 42},
		{ID: 2, Opponent: "ave62", IsRunning: true, CreatedAt: inSeason, Ruleset: 42},
	}}
	if r, ok := inviteRuleset(g, "luremus", games); !ok || r != 42 {
		t.Errorf("got %d %v, want the ruleset of the league games under way", r, ok)
	}
	g.LeagueID = 999
	if _, ok := inviteRuleset(g, "luremus", nil); ok {
		t.Error("unknown league with no games must not be invitable")
	}
}

func TestResignedScores(t *testing.T) {
	cases := []struct {
		name            string
		my, opp         int
		resignedBy      string
		wantMy, wantOpp int
	}{
		{"opponent resigned early", 80, 60, "opponent", 150, 0},
		{"opponent resigned far behind", 450, 200, "opponent", 450, 150},
		{"opponent resigned while ahead", 100, 300, "opponent", 150, 0},
		{"I resigned early", 60, 80, "me", 0, 150},
		{"I resigned far behind", 200, 450, "me", 150, 450},
	}
	for _, c := range cases {
		game := &GameSummary{Scores: [2]int{c.my, c.opp}, OpponentID: 7, LastMove: MoveInfo{MoveType: "resign", UserID: 1}}
		if c.resignedBy == "opponent" {
			game.LastMove.UserID = 7
		}
		my, opp, who := resignedScores(game)
		if my != c.wantMy || opp != c.wantOpp || who != c.resignedBy {
			t.Errorf("%s: got %d-%d by %s, want %d-%d by %s", c.name, my, opp, who, c.wantMy, c.wantOpp, c.resignedBy)
		}
	}
}

func TestFlexNumber(t *testing.T) {
	var g rawGame
	for _, raw := range []string{
		`{"id":1,"created":1760000000.0,"ruleset":1}`,
		`{"id":1,"created":"1760000000","ruleset":"1"}`,
	} {
		if err := json.Unmarshal([]byte(raw), &g); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if s := g.toSummary(); s.CreatedAt != 1760000000 || s.Ruleset != 1 {
			t.Errorf("%s: summary = %+v", raw, s)
		}
	}
	g = rawGame{}
	if err := json.Unmarshal([]byte(`{"id":1,"created":null,"ruleset":{"odd":true}}`), &g); err != nil {
		t.Fatalf("odd shapes must not fail the decode: %v", err)
	}
	if s := g.toSummary(); s.CreatedAt != 0 || s.Ruleset != -1 {
		t.Errorf("summary = %+v, want unknown start and ruleset", s)
	}
}

// fakeWordfeud is a stand-in for the Wordfeud API.
type fakeWordfeud struct {
	games    []map[string]any
	sent     []any
	received []any
	invited  []map[string]any
	inviteAs map[string]any // non-nil: error content returned for invites
}

func (f *fakeWordfeud) client(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok := func(content any) {
			json.NewEncoder(w).Encode(map[string]any{"status": "success", "content": content})
		}
		switch r.URL.Path {
		case "/wf/user/games/":
			ok(map[string]any{"games": f.games})
		case "/wf/user/status/":
			ok(map[string]any{"invites_sent": f.sent, "invites_received": f.received})
		case "/wf/invite/new/":
			if f.inviteAs != nil {
				json.NewEncoder(w).Encode(map[string]any{"status": "error", "content": f.inviteAs})
				return
			}
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			f.invited = append(f.invited, body)
			ok(map[string]any{})
		default:
			t.Errorf("unexpected Wordfeud call %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{httpClient: srv.Client(), baseURL: srv.URL + "/wf"}
}

func wfGame(id int64, opponent string, running bool, my, opp int) map[string]any {
	return map[string]any{
		"id": id, "is_running": running, "current_player": 0, "created": float64(inSeason), "ruleset": 1,
		"players": []map[string]any{
			{"username": "luremus", "id": 1, "score": my, "is_local": true},
			{"username": opponent, "id": 2, "score": opp},
		},
		"last_move": map[string]any{"user_id": 2, "move_type": "move", "points": 10},
	}
}

// fakeLeagueSite is a stand-in for the league site.
type fakeLeagueSite struct {
	group     *wloh.Group
	finds     int
	gets      int
	submitted []wloh.Match
	submitErr error
	loginErr  error
	findErr   error
}

func (f *fakeLeagueSite) copyGroup() *wloh.Group {
	g := *f.group
	g.Matches = append([]wloh.Match(nil), f.group.Matches...)
	return &g
}

func (f *fakeLeagueSite) FindGroup(ctx context.Context, username string) (*wloh.Group, error) {
	f.finds++
	if f.findErr != nil {
		return nil, f.findErr
	}
	if f.group.Player(username) == "" {
		return nil, wloh.ErrPlayerNotFound
	}
	return f.copyGroup(), nil
}

func (f *fakeLeagueSite) GetGroup(ctx context.Context, serieID int64) (*wloh.Group, error) {
	f.gets++
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.copyGroup(), nil
}

func (f *fakeLeagueSite) CheckLogin(ctx context.Context, username, email string) error {
	return f.loginErr
}

func (f *fakeLeagueSite) SubmitResult(ctx context.Context, username, email string, serieID int64, opponent string, my, opp int) (*wloh.Group, error) {
	if f.submitErr != nil {
		return f.copyGroup(), f.submitErr
	}
	m := wloh.Match{Home: username, Away: opponent, HomeScore: my, AwayScore: opp}
	f.submitted = append(f.submitted, m)
	f.group.Matches = append(f.group.Matches, m)
	return f.copyGroup(), nil
}

type leagueFixture struct {
	db     *sql.DB
	user   *auth.User
	wf     *fakeWordfeud
	site   *fakeLeagueSite
	league *League
	now    time.Time
}

func newLeagueFixture(t *testing.T) *leagueFixture {
	t.Helper()
	fx := &leagueFixture{
		db:   setupTestDB(t),
		wf:   &fakeWordfeud{},
		site: &fakeLeagueSite{group: testGroup()},
		now:  leagueDeadline.Add(-10 * 24 * time.Hour),
	}
	fx.user = createTestUser(t, fx.db)
	fx.league = &League{DB: fx.db, WF: fx.wf.client(t), Site: fx.site, Now: func() time.Time { return fx.now }}
	return fx
}

func (fx *leagueFixture) setPref(t *testing.T, key, value string) {
	t.Helper()
	enc, err := encryption.EncryptField(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.SetPreference(fx.db, fx.user.ID, key, enc); err != nil {
		t.Fatal(err)
	}
}

// configure sets up the league account and a connected Wordfeud session.
func (fx *leagueFixture) configure(t *testing.T) {
	t.Helper()
	fx.setPref(t, PrefLeagueUsername, "luremus")
	fx.setPref(t, PrefLeagueEmail, "me@example.com")
	fx.setPref(t, "wordfeud_session_token", "token")
}

func (fx *leagueFixture) call(t *testing.T, h http.HandlerFunc, method, body string) (int, map[string]any) {
	t.Helper()
	req := requestWithUser(httptest.NewRequest(method, "/api/wordfeud/league", bytes.NewBufferString(body)), fx.user)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	return w.Code, resp
}

func TestLeagueViewNotConfigured(t *testing.T) {
	fx := newLeagueFixture(t)
	view, err := fx.league.View(context.Background(), fx.user.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if view.Configured || view.Group != nil || fx.site.finds != 0 {
		t.Errorf("view = %+v, site lookups = %d", view, fx.site.finds)
	}
}

func TestLeagueViewCachesAndRefreshes(t *testing.T) {
	fx := newLeagueFixture(t)
	fx.configure(t)
	fx.wf.games = []map[string]any{wfGame(1, "tjobias", true, 100, 80)}
	fx.wf.sent = []any{map[string]any{"id": 9, "invitee": "ave62"}}
	fx.wf.received = []any{map[string]any{"id": 10, "inviter": map[string]any{"username": "Illkl"}}}

	view, err := fx.league.View(context.Background(), fx.user.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Configured || !view.CanReport || !view.CanInvite || !view.WordfeudConnected || view.Group.Season != 398 {
		t.Errorf("view = %+v", view)
	}
	if m := statusOf(view.Matches, "tjobias"); m.Status != MatchPlaying {
		t.Errorf("tjobias = %+v", m)
	}
	if m := statusOf(view.Matches, "ave62"); m.Status != MatchInviteSent {
		t.Errorf("ave62 = %+v", m)
	}
	if m := statusOf(view.Matches, "Illkl"); m.Status != MatchInviteReceived {
		t.Errorf("Illkl = %+v", m)
	}

	// A second view within the cache window does not hit the league site.
	if _, err := fx.league.View(context.Background(), fx.user.ID, false); err != nil {
		t.Fatal(err)
	}
	if fx.site.finds != 1 || fx.site.gets != 0 {
		t.Errorf("site lookups = %d finds, %d gets; want 1, 0", fx.site.finds, fx.site.gets)
	}
	// A refresh re-reads the known group by id.
	if _, err := fx.league.View(context.Background(), fx.user.ID, true); err != nil {
		t.Fatal(err)
	}
	if fx.site.finds != 1 || fx.site.gets != 1 {
		t.Errorf("after refresh: %d finds, %d gets; want 1, 1", fx.site.finds, fx.site.gets)
	}
	// After the deadline the new season's group is searched for.
	fx.now = leagueDeadline.Add(time.Hour)
	if _, err := fx.league.View(context.Background(), fx.user.ID, true); err != nil {
		t.Fatal(err)
	}
	if fx.site.finds != 2 {
		t.Errorf("after deadline: %d finds, want 2", fx.site.finds)
	}
}

func TestLeagueViewFallsBackToStoredGroup(t *testing.T) {
	fx := newLeagueFixture(t)
	fx.configure(t)
	if _, err := fx.league.View(context.Background(), fx.user.ID, false); err != nil {
		t.Fatal(err)
	}
	fx.site.findErr = errors.New("site down")
	view, err := fx.league.View(context.Background(), fx.user.ID, true)
	if err != nil {
		t.Fatalf("expected the stored copy, got %v", err)
	}
	if !view.Stale || view.Group == nil {
		t.Errorf("view = %+v, want a stale stored group", view)
	}
}

func TestLeagueViewWithoutWordfeud(t *testing.T) {
	fx := newLeagueFixture(t)
	fx.setPref(t, PrefLeagueUsername, "luremus")
	view, err := fx.league.View(context.Background(), fx.user.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if view.WordfeudConnected || view.CanInvite || view.CanReport || len(view.Matches) != 5 {
		t.Errorf("view = %+v", view)
	}
}

func TestLeagueConfigHandler(t *testing.T) {
	fx := newLeagueFixture(t)
	h := LeagueConfigHandler(fx.league)

	if code, resp := fx.call(t, h, http.MethodPut, `{"username":"nobody"}`); code != http.StatusNotFound || resp["code"] != "player_not_found" {
		t.Errorf("unknown player: %d %v", code, resp)
	}
	fx.site.loginErr = wloh.ErrLoginFailed
	if code, resp := fx.call(t, h, http.MethodPut, `{"username":"luremus","email":"bad@example.com"}`); code != http.StatusUnauthorized || resp["code"] != "league_login_failed" {
		t.Errorf("bad email: %d %v", code, resp)
	}
	if cfg, _ := loadLeagueConfig(fx.db, fx.user.ID); cfg.username != "" {
		t.Errorf("config saved despite failed login: %+v", cfg)
	}

	fx.site.loginErr = nil
	if code, resp := fx.call(t, h, http.MethodPut, `{"username":" luremus ","email":"me@example.com"}`); code != http.StatusOK {
		t.Fatalf("save: %d %v", code, resp)
	}
	cfg, _ := loadLeagueConfig(fx.db, fx.user.ID)
	if cfg.username != "luremus" || cfg.email != "me@example.com" {
		t.Errorf("config = %+v", cfg)
	}
	var raw string
	fx.db.QueryRow(`SELECT value FROM user_preferences WHERE user_id = ? AND key = ?`, fx.user.ID, PrefLeagueEmail).Scan(&raw)
	if !strings.HasPrefix(raw, "enc:") {
		t.Errorf("email stored unencrypted: %q", raw)
	}

	// Saving again without an email keeps the stored one.
	if code, _ := fx.call(t, h, http.MethodPut, `{"username":"Luremus"}`); code != http.StatusOK {
		t.Errorf("re-save: %d", code)
	}
	if cfg, _ := loadLeagueConfig(fx.db, fx.user.ID); cfg.username != "Luremus" || cfg.email != "me@example.com" {
		t.Errorf("config after re-save = %+v", cfg)
	}

	if code, _ := fx.call(t, h, http.MethodPut, `{"username":""}`); code != http.StatusOK {
		t.Errorf("remove: %d", code)
	}
	if cfg, _ := loadLeagueConfig(fx.db, fx.user.ID); cfg.username != "" || cfg.email != "" {
		t.Errorf("config not removed: %+v", cfg)
	}
}

func TestLeagueInviteHandler(t *testing.T) {
	fx := newLeagueFixture(t)
	fx.configure(t)
	fx.wf.games = []map[string]any{wfGame(1, "tjobias", true, 100, 80)}
	h := LeagueInviteHandler(fx.league)

	if code, resp := fx.call(t, h, http.MethodPost, `{"opponent":"AVE62"}`); code != http.StatusOK || resp["opponent"] != "ave62" {
		t.Fatalf("invite: %d %v", code, resp)
	}
	if len(fx.wf.invited) != 1 {
		t.Fatalf("invites sent = %v", fx.wf.invited)
	}
	if inv := fx.wf.invited[0]; inv["invitee"] != "ave62" || inv["ruleset"] != float64(RulesetNorwegian) || inv["board_type"] != BoardNormal {
		t.Errorf("invite payload = %v", inv)
	}

	// No invite for someone already playing, outside the group, or myself.
	for _, opponent := range []string{"tjobias", "stranger", "luremus"} {
		if code, _ := fx.call(t, h, http.MethodPost, `{"opponent":"`+opponent+`"}`); code == http.StatusOK {
			t.Errorf("invite to %s was accepted", opponent)
		}
	}
	// A pending invitation blocks a second one.
	fx.wf.sent = []any{map[string]any{"invitee": "Illkl"}}
	if code, resp := fx.call(t, h, http.MethodPost, `{"opponent":"Illkl"}`); code != http.StatusConflict || resp["code"] != "already_started" {
		t.Errorf("duplicate invite: %d %v", code, resp)
	}
	if len(fx.wf.invited) != 1 {
		t.Errorf("unexpected invites went out: %v", fx.wf.invited)
	}

	fx.wf.inviteAs = map[string]any{"type": "user_not_found", "message": "No such user"}
	if code, resp := fx.call(t, h, http.MethodPost, `{"opponent":"jenscb"}`); code != http.StatusBadGateway || resp["code"] != "invite_refused" || resp["detail"] != "user_not_found No such user" {
		t.Errorf("refused invite: %d %v", code, resp)
	}
}

func TestLeagueReportHandler(t *testing.T) {
	fx := newLeagueFixture(t)
	fx.configure(t)
	h := LeagueReportHandler(fx.league)

	for _, body := range []string{
		`{"opponent":"tjobias"}`,
		`{"opponent":"tjobias","my_score":-1,"opp_score":10}`,
		`{"opponent":"tjobias","my_score":10,"opp_score":99999}`,
		`{"opponent":"stranger","my_score":10,"opp_score":20}`,
		`{"opponent":"luremus","my_score":10,"opp_score":20}`,
	} {
		if code, _ := fx.call(t, h, http.MethodPost, body); code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", body, code)
		}
	}
	// 1roxy1 already has a registered result.
	if code, resp := fx.call(t, h, http.MethodPost, `{"opponent":"1roxy1","my_score":1,"opp_score":2}`); code != http.StatusConflict || resp["code"] != "already_reported" {
		t.Errorf("already reported: %d %v", code, resp)
	}
	if len(fx.site.submitted) != 0 {
		t.Fatalf("results posted by rejected requests: %v", fx.site.submitted)
	}

	if code, resp := fx.call(t, h, http.MethodPost, `{"opponent":"TJOBIAS","my_score":592,"opp_score":440}`); code != http.StatusOK {
		t.Fatalf("report: %d %v", code, resp)
	}
	want := wloh.Match{Home: "luremus", Away: "tjobias", HomeScore: 592, AwayScore: 440}
	if len(fx.site.submitted) != 1 || fx.site.submitted[0] != want {
		t.Errorf("submitted = %v, want %v", fx.site.submitted, want)
	}
	// The stored group now carries the result, so the view shows it at once.
	view, _ := fx.league.View(context.Background(), fx.user.ID, false)
	if m := statusOf(view.Matches, "tjobias"); m.Status != MatchReported || m.MyScore != 592 {
		t.Errorf("tjobias after report = %+v", m)
	}

	fx.site.submitErr = &wloh.ResultError{Notice: "Follow the instructions"}
	if code, resp := fx.call(t, h, http.MethodPost, `{"opponent":"ave62","my_score":1,"opp_score":2}`); code != http.StatusBadGateway || resp["code"] != "not_registered" || resp["detail"] != "Follow the instructions" {
		t.Errorf("not registered: %d %v", code, resp)
	}
}

func TestLeagueReportNeedsEmail(t *testing.T) {
	fx := newLeagueFixture(t)
	fx.setPref(t, PrefLeagueUsername, "luremus")
	code, resp := fx.call(t, LeagueReportHandler(fx.league), http.MethodPost, `{"opponent":"tjobias","my_score":1,"opp_score":2}`)
	if code != http.StatusBadRequest || resp["code"] != "not_configured" {
		t.Errorf("got %d %v", code, resp)
	}
}

func TestLeagueNotifier(t *testing.T) {
	fx := newLeagueFixture(t)
	fx.configure(t)
	fx.wf.games = []map[string]any{
		wfGame(1, "tjobias", true, 100, 80),
		wfGame(2, "ave62", false, 480, 469),
		wfGame(3, "1roxy1", false, 500, 400), // already registered
	}
	var sent []push.Notification
	quiet := false
	n := &LeagueNotifier{
		League: fx.league,
		Send: func(db *sql.DB, userID int64, payload []byte) error {
			var note push.Notification
			json.Unmarshal(payload, &note)
			sent = append(sent, note)
			return nil
		},
		Quiet: func(map[string]string, time.Time) bool { return quiet },
	}

	quiet = true
	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 0 {
		t.Fatalf("pushed during quiet hours: %+v", sent)
	}

	quiet = false
	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || !strings.Contains(sent[0].Title, "ave62") || !strings.Contains(sent[0].Body, "480–469") || sent[0].URL != leagueTabURL {
		t.Fatalf("pushes = %+v, want one for the finished game against ave62", sent)
	}
	// The same finished game is not announced twice.
	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 {
		t.Fatalf("repeat push: %+v", sent)
	}

	// A new group id after the deadline announces the new season once.
	fx.now = leagueDeadline.Add(2 * time.Hour)
	next := testGroup()
	next.SerieID, next.Season, next.Division = 968000, 399, 4
	next.Deadline = leagueDeadline.Add(seasonLength)
	next.Matches = nil
	fx.site.group = next
	fx.wf.games = nil
	for i := 0; i < 2; i++ {
		if err := n.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(sent) != 2 || !strings.Contains(sent[1].Title, "399") || !strings.Contains(sent[1].Body, "division 4") {
		t.Fatalf("pushes = %+v, want one new-season push", sent)
	}
}

func TestLeagueNotifierRetriesFailedPush(t *testing.T) {
	fx := newLeagueFixture(t)
	fx.configure(t)
	fx.wf.games = []map[string]any{wfGame(2, "ave62", false, 480, 469)}
	attempts, fail := 0, true
	n := &LeagueNotifier{League: fx.league, Send: func(*sql.DB, int64, []byte) error {
		attempts++
		if fail {
			return errors.New("no subscriptions")
		}
		return nil
	}}
	n.Run(context.Background())
	fail = false
	n.Run(context.Background())
	n.Run(context.Background())
	if attempts != 2 {
		t.Errorf("send attempts = %d, want 2 (one failure, one delivery)", attempts)
	}
}
