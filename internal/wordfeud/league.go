package wordfeud

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/wloh"
)

// Preference keys for the Wordfeud League of Honour account. Both values are
// encrypted at rest. The site logs in with username + email, no password.
const (
	PrefLeagueUsername = "wloh_username"
	PrefLeagueEmail    = "wloh_email"
)

// Match states in the league view, in the order a match moves through them.
const (
	MatchNotStarted     = "not_started"
	MatchInviteSent     = "invite_sent"
	MatchInviteReceived = "invite_received"
	MatchPlaying        = "playing"
	MatchToReport       = "to_report"
	MatchReported       = "reported"
)

const (
	// seasonLength is how long a league season lasts.
	seasonLength = 14 * 24 * time.Hour
	// seasonSlack admits games started just before the previous deadline,
	// when groups are sometimes published early.
	seasonSlack = 12 * time.Hour
	// leagueViewMaxAge is how old the stored group may be when a user opens
	// the league tab before it is fetched again.
	leagueViewMaxAge = 10 * time.Minute

	// League rules for a game the opponent abandoned: the winner registers
	// 150–0, or their real score against the loser's minus 50, whichever
	// gives the bigger margin.
	resignWinScore = 150
	resignPenalty  = 50
)

// leagueRulesets maps a WLoH league id to the Wordfeud ruleset its games are
// played with. Leagues missing here can be viewed but not invited from.
var leagueRulesets = map[int]int{
	1: RulesetNorwegian,
	2: RulesetEnglishIntl,
	3: RulesetDanish,
	4: RulesetSwedish,
	7: RulesetEnglishUS,
	8: RulesetDutch,
}

// LeagueSite is the part of the league site the league view needs.
type LeagueSite interface {
	FindGroup(ctx context.Context, username string) (*wloh.Group, error)
	GetGroup(ctx context.Context, serieID int64) (*wloh.Group, error)
	CheckLogin(ctx context.Context, username, email string) error
	SubmitResult(ctx context.Context, username, email string, serieID int64, opponent string, myScore, oppScore int) (*wloh.Group, error)
}

// League joins a user's league group with their Wordfeud games.
type League struct {
	DB   *sql.DB
	WF   *Client
	Site LeagueSite
	Now  func() time.Time
}

// NewLeague returns a League using the live league site.
func NewLeague(db *sql.DB, wf *Client) *League {
	return &League{DB: db, WF: wf, Site: wloh.NewClient(), Now: time.Now}
}

// LeagueMatch is the user's match against one opponent in the group.
type LeagueMatch struct {
	Opponent string `json:"opponent"`
	Status   string `json:"status"`
	GameID   int64  `json:"game_id,omitempty"`
	MyScore  int    `json:"my_score"`
	OppScore int    `json:"opp_score"`
	IsMyTurn bool   `json:"is_my_turn,omitempty"`
	// Resigned is "me" or "opponent" when the game ended by resignation;
	// the scores are then the ones the league rules say to register.
	Resigned string `json:"resigned,omitempty"`
}

// LeagueView is the response of the league endpoint.
type LeagueView struct {
	Configured        bool          `json:"configured"`
	Username          string        `json:"username,omitempty"`
	CanReport         bool          `json:"can_report"`
	CanInvite         bool          `json:"can_invite"`
	WordfeudConnected bool          `json:"wordfeud_connected"`
	Group             *wloh.Group   `json:"group,omitempty"`
	Matches           []LeagueMatch `json:"matches"`
	FetchedAt         string        `json:"fetched_at,omitempty"`
	// Stale is set when the league site could not be reached and Group is
	// the last copy that was stored.
	Stale bool `json:"stale,omitempty"`

	// inviteRuleset is the Wordfeud ruleset invitations are sent with; only
	// meaningful when CanInvite is set.
	inviteRuleset int
}

type leagueConfig struct {
	username string
	email    string
}

func loadLeagueConfig(db *sql.DB, userID int64) (leagueConfig, error) {
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		return leagueConfig{}, err
	}
	return leagueConfig{
		username: encryption.DecryptLenient(prefs[PrefLeagueUsername]),
		email:    encryption.DecryptLenient(prefs[PrefLeagueEmail]),
	}, nil
}

// leagueSnapshot is the stored copy of a user's group.
type leagueSnapshot struct {
	group     *wloh.Group
	fetchedAt time.Time
}

func loadLeagueSnapshot(db *sql.DB, userID int64) (*leagueSnapshot, error) {
	var raw, fetched string
	err := db.QueryRow(`SELECT snapshot, fetched_at FROM wordfeud_league WHERE user_id = ?`, userID).Scan(&raw, &fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	plain, err := encryption.DecryptField(raw)
	if err != nil {
		return nil, nil // unreadable copy: treat as absent and fetch again
	}
	var g wloh.Group
	if err := json.Unmarshal([]byte(plain), &g); err != nil || g.SerieID == 0 {
		return nil, nil
	}
	at, _ := time.Parse(time.RFC3339, fetched)
	return &leagueSnapshot{group: &g, fetchedAt: at}, nil
}

func saveLeagueSnapshot(db *sql.DB, userID int64, g *wloh.Group, at time.Time) error {
	data, err := json.Marshal(g)
	if err != nil {
		return err
	}
	enc, err := encryption.EncryptField(string(data))
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO wordfeud_league (user_id, serie_id, season, snapshot, fetched_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET serie_id = excluded.serie_id, season = excluded.season,
			snapshot = excluded.snapshot, fetched_at = excluded.fetched_at`,
		userID, g.SerieID, g.Season, enc, at.UTC().Format(time.RFC3339))
	return err
}

// group returns the user's current group, fetching it when the stored copy
// is older than maxAge. When the site can't be reached the stored copy is
// returned with stale set.
func (l *League) group(ctx context.Context, userID int64, username string, maxAge time.Duration) (g *wloh.Group, fetchedAt time.Time, stale bool, err error) {
	now := l.Now()
	snap, err := loadLeagueSnapshot(l.DB, userID)
	if err != nil {
		return nil, time.Time{}, false, err
	}
	if snap != nil && snap.group.Player(username) == "" {
		snap = nil // the configured username changed
	}
	if snap != nil && now.Sub(snap.fetchedAt) < maxAge {
		return snap.group, snap.fetchedAt, false, nil
	}

	var fresh *wloh.Group
	// While the season runs the group id is stable, so one page is enough.
	// After the deadline, search for the new season's group.
	if snap != nil && now.Before(snap.group.Deadline) {
		fresh, err = l.Site.GetGroup(ctx, snap.group.SerieID)
		if err == nil && fresh.Player(username) == "" {
			fresh, err = l.Site.FindGroup(ctx, username)
		}
	} else {
		fresh, err = l.Site.FindGroup(ctx, username)
	}
	if err != nil {
		if snap != nil {
			log.Printf("wordfeud league: refresh for user %d failed, using stored copy: %v", userID, err)
			return snap.group, snap.fetchedAt, true, nil
		}
		return nil, time.Time{}, false, err
	}
	if err := saveLeagueSnapshot(l.DB, userID, fresh, now); err != nil {
		log.Printf("wordfeud league: store group for user %d: %v", userID, err)
	}
	return fresh, now, false, nil
}

// withSession runs fn with the user's Wordfeud session token, logging in
// again once if the session has expired. It returns errNotConnected when no
// Wordfeud account is connected.
func withSession(db *sql.DB, client *Client, userID int64, fn func(token string) error) error {
	token, err := getSessionToken(db, userID)
	if err != nil {
		return err
	}
	if token == "" {
		return errNotConnected
	}
	err = fn(token)
	if errors.Is(err, ErrSessionExpired) {
		newToken, reloginErr := autoRelogin(db, userID, client)
		if reloginErr != nil {
			return fmt.Errorf("%w (auto-relogin: %v)", ErrSessionExpired, reloginErr)
		}
		err = fn(newToken)
	}
	return err
}

var errNotConnected = errors.New("wordfeud: no account connected")

// View builds the league view for a user. A refresh forces a new read of the
// league site.
func (l *League) View(ctx context.Context, userID int64, refresh bool) (*LeagueView, error) {
	cfg, err := loadLeagueConfig(l.DB, userID)
	if err != nil {
		return nil, err
	}
	view := &LeagueView{Matches: []LeagueMatch{}}
	if cfg.username == "" {
		return view, nil
	}
	view.Configured = true
	view.Username = cfg.username
	view.CanReport = cfg.email != ""

	maxAge := leagueViewMaxAge
	if refresh {
		maxAge = 0
	}
	g, fetchedAt, stale, err := l.group(ctx, userID, cfg.username, maxAge)
	if err != nil {
		return nil, err
	}
	view.Group = g
	view.Stale = stale
	view.FetchedAt = fetchedAt.UTC().Format(time.RFC3339)

	var games *GamesResult
	var invites *Invites
	err = withSession(l.DB, l.WF, userID, func(token string) error {
		var err error
		if games, err = l.WF.GetGames(token); err != nil {
			return err
		}
		// Pending invitations are a nicety; the view works without them.
		if inv, err := l.WF.GetInvites(token); err == nil {
			invites = inv
		} else if errors.Is(err, ErrSessionExpired) {
			return err
		} else {
			log.Printf("wordfeud league: pending invitations unavailable for user %d: %v", userID, err)
		}
		return nil
	})
	switch {
	case err == nil:
		view.WordfeudConnected = true
	case errors.Is(err, errNotConnected):
	default:
		log.Printf("wordfeud league: games unavailable for user %d: %v", userID, err)
	}
	ruleset, hasRuleset := inviteRuleset(g, cfg.username, games)
	view.CanInvite = view.WordfeudConnected && hasRuleset
	view.inviteRuleset = ruleset
	view.Matches = buildLeagueMatches(g, cfg.username, games, invites)
	return view, nil
}

// buildLeagueMatches pairs each opponent in the group with the user's
// Wordfeud game against them, if any. games and invites may be nil.
func buildLeagueMatches(g *wloh.Group, me string, games *GamesResult, invites *Invites) []LeagueMatch {
	matches := make([]LeagueMatch, 0, len(g.Standings))
	for _, s := range g.Standings {
		if strings.EqualFold(s.Name, me) {
			continue
		}
		m := LeagueMatch{Opponent: s.Name, Status: MatchNotStarted}
		game := leagueGame(g, s.Name, games)
		if game != nil {
			m.GameID = game.ID
			m.MyScore, m.OppScore = game.Scores[0], game.Scores[1]
		}
		if my, opp, ok := g.Result(me, s.Name); ok {
			m.Status = MatchReported
			m.MyScore, m.OppScore = my, opp
		} else if game != nil && game.IsRunning {
			m.Status = MatchPlaying
			m.IsMyTurn = game.IsMyTurn
		} else if game != nil {
			m.Status = MatchToReport
			if game.LastMove.MoveType == "resign" {
				m.MyScore, m.OppScore, m.Resigned = resignedScores(game)
			}
		} else if invites != nil && containsFold(invites.Received, s.Name) {
			m.Status = MatchInviteReceived
		} else if invites != nil && containsFold(invites.Sent, s.Name) {
			m.Status = MatchInviteSent
		}
		matches = append(matches, m)
	}
	return matches
}

// leagueGame picks the Wordfeud game that is the league match against
// opponent: a game in the league's language started during this season. With
// several candidates the earliest wins, as the league game is the one both
// players start when the season opens.
func leagueGame(g *wloh.Group, opponent string, games *GamesResult) *GameSummary {
	if games == nil {
		return nil
	}
	ruleset, hasRuleset := leagueRulesets[g.LeagueID]
	if hasRuleset {
		if game := pickLeagueGame(g, opponent, games, ruleset); game != nil {
			return game
		}
	}
	// No game in the expected language: accept any, rather than show a game
	// in progress as not started (which would invite the opponent twice).
	return pickLeagueGame(g, opponent, games, -1)
}

// pickLeagueGame is leagueGame for one ruleset; -1 accepts every ruleset.
func pickLeagueGame(g *wloh.Group, opponent string, games *GamesResult, ruleset int) *GameSummary {
	start := g.Deadline.Add(-seasonLength - seasonSlack).Unix()
	var best *GameSummary
	consider := func(list []GameSummary) {
		for i := range list {
			c := &list[i]
			if !strings.EqualFold(c.Opponent, opponent) {
				continue
			}
			if ruleset >= 0 && c.Ruleset >= 0 && c.Ruleset != ruleset {
				continue
			}
			// Without a start time, fall back to the last activity for
			// finished games; a running game is always current.
			started := c.CreatedAt
			if started == 0 && !c.IsRunning {
				started = c.EndedAt
			}
			if !g.Deadline.IsZero() && started != 0 && started < start {
				continue
			}
			if best == nil || (c.CreatedAt != 0 && (best.CreatedAt == 0 || c.CreatedAt < best.CreatedAt)) {
				best = c
			}
		}
	}
	consider(games.Active)
	consider(games.Finished)
	return best
}

// inviteRuleset returns the Wordfeud ruleset to invite league opponents
// with. The league games already under way are the best evidence of it; the
// league table is the fallback when there are none yet.
func inviteRuleset(g *wloh.Group, me string, games *GamesResult) (int, bool) {
	counts := map[int]int{}
	for _, s := range g.Standings {
		if strings.EqualFold(s.Name, me) {
			continue
		}
		if game := leagueGame(g, s.Name, games); game != nil && game.Ruleset >= 0 {
			counts[game.Ruleset]++
		}
	}
	best, bestCount := 0, 0
	for ruleset, n := range counts {
		if n > bestCount || (n == bestCount && ruleset < best) {
			best, bestCount = ruleset, n
		}
	}
	if bestCount > 0 {
		return best, true
	}
	ruleset, ok := leagueRulesets[g.LeagueID]
	return ruleset, ok
}

// resignedScores returns the scores the league rules prescribe for a game
// that ended by resignation, from the user's side, and who resigned.
func resignedScores(game *GameSummary) (my, opp int, who string) {
	my, opp = game.Scores[0], game.Scores[1]
	if game.LastMove.UserID == game.OpponentID {
		if opp-resignPenalty < 0 || my-(opp-resignPenalty) <= resignWinScore {
			return resignWinScore, 0, "opponent"
		}
		return my, opp - resignPenalty, "opponent"
	}
	if my-resignPenalty < 0 || opp-(my-resignPenalty) <= resignWinScore {
		return 0, resignWinScore, "me"
	}
	return my - resignPenalty, opp, "me"
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}
