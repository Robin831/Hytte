package wordfeud

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/quiethours"
	"github.com/Robin831/Hytte/internal/wloh"
)

const (
	// LeagueNotifyInterval is how often league games are checked for results
	// to report and for a new season.
	LeagueNotifyInterval = 30 * time.Minute
	// leagueSyncMaxAge is how old the stored group may get during a season
	// before the background loop reads the league site again.
	leagueSyncMaxAge = 3 * time.Hour
	// leagueRolloverMaxAge applies once the deadline has passed, when the
	// next season's groups are about to be published.
	leagueRolloverMaxAge = time.Hour

	leagueNotifySeason   = "season"
	leagueNotifyFinished = "finished"

	leagueTabURL = "/wordfeud?tab=league"
)

// LeagueNotifier pushes league news: a new season's group and finished games
// whose result still has to be registered. Send and Quiet are injectable for
// tests.
type LeagueNotifier struct {
	League *League
	Send   func(db *sql.DB, userID int64, payload []byte) error
	Quiet  func(prefs map[string]string, at time.Time) bool
}

// NewLeagueNotifier returns a notifier wired to real push and quiet hours.
func NewLeagueNotifier(l *League) *LeagueNotifier {
	return &LeagueNotifier{League: l, Send: leagueSend, Quiet: quiethours.IsActiveWithPrefsAt}
}

// RunLeagueLoop checks every league user shortly after start and then every
// interval until ctx ends.
func RunLeagueLoop(ctx context.Context, l *League, interval time.Duration) {
	n := NewLeagueNotifier(l)
	wait := 2 * time.Minute
	for {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if err := n.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("wordfeud league: notify pass: %v", err)
		}
		wait = interval
	}
}

// Run does one pass over every user with a league username configured.
func (n *LeagueNotifier) Run(ctx context.Context) error {
	rows, err := n.League.DB.QueryContext(ctx, `SELECT user_id FROM user_preferences WHERE key = ? AND value != ''`, PrefLeagueUsername)
	if err != nil {
		return fmt.Errorf("list league users: %w", err)
	}
	var users []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		users = append(users, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range users {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := n.notifyUser(ctx, id); err != nil {
			log.Printf("wordfeud league: user %d: %v", id, err)
		}
	}
	return nil
}

func (n *LeagueNotifier) notifyUser(ctx context.Context, userID int64) error {
	l := n.League
	now := l.Now()
	cfg, err := loadLeagueConfig(l.DB, userID)
	if err != nil || cfg.username == "" {
		return err
	}
	prev, err := loadLeagueSnapshot(l.DB, userID)
	if err != nil {
		return err
	}
	maxAge := leagueSyncMaxAge
	if prev != nil && now.After(prev.group.Deadline) {
		maxAge = leagueRolloverMaxAge
	}
	g, fetchedAt, stale, err := l.group(ctx, userID, cfg.username, maxAge)
	if err != nil {
		return err
	}

	prefs, err := auth.GetPreferences(l.DB, userID)
	if err != nil {
		return err
	}
	if n.Quiet != nil && n.Quiet(prefs, now) {
		return nil // hold everything; the next pass after quiet hours sends it
	}
	lang := prefs["ui_language"]

	// A new group id after one was already known means a new season started.
	if prev != nil && !stale && prev.group.SerieID != g.SerieID && prev.group.Player(cfg.username) != "" {
		n.sendOnce(userID, g.SerieID, leagueNotifySeason, "", leagueSeasonText(lang, g.Season, g.Division, len(g.Standings)-1))
	}

	var games *GamesResult
	err = withSession(l.DB, l.WF, userID, func(token string) error {
		var err error
		games, err = l.WF.GetGames(token)
		return err
	})
	if errors.Is(err, errNotConnected) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("fetch games: %w", err)
	}
	pending := n.unreported(userID, g, cfg.username, games)
	if len(pending) > 0 && now.Sub(fetchedAt) > leagueViewMaxAge {
		// The result may have been registered on the league site since the
		// last read; look again before nagging about it.
		if g, _, _, err = l.group(ctx, userID, cfg.username, leagueViewMaxAge); err != nil {
			return err
		}
		pending = n.unreported(userID, g, cfg.username, games)
	}
	for _, m := range pending {
		n.sendOnce(userID, g.SerieID, leagueNotifyFinished, strconv.FormatInt(m.GameID, 10),
			leagueFinishedText(lang, m.Opponent, m.MyScore, m.OppScore))
	}
	return nil
}

// unreported lists finished league games with no registered result that the
// user has not been told about yet.
func (n *LeagueNotifier) unreported(userID int64, g *wloh.Group, me string, games *GamesResult) []LeagueMatch {
	var out []LeagueMatch
	for _, m := range buildLeagueMatches(g, me, games, nil) {
		if m.Status == MatchToReport && !n.notified(userID, g.SerieID, leagueNotifyFinished, strconv.FormatInt(m.GameID, 10)) {
			out = append(out, m)
		}
	}
	return out
}

// notified reports whether a push for the event was already delivered. A
// lookup failure counts as delivered, so a DB problem can't cause repeats.
func (n *LeagueNotifier) notified(userID, serieID int64, kind, ref string) bool {
	var one int
	err := n.League.DB.QueryRow(`SELECT 1 FROM wordfeud_league_notified WHERE user_id = ? AND serie_id = ? AND kind = ? AND ref = ?`,
		userID, serieID, kind, ref).Scan(&one)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Printf("wordfeud league: check notified for user %d: %v", userID, err)
		return true
	}
	return err == nil
}

// sendOnce pushes a notification unless one was already delivered for the
// same event. A failed send is not recorded, so the next pass retries it.
func (n *LeagueNotifier) sendOnce(userID, serieID int64, kind, ref string, note push.Notification) {
	db := n.League.DB
	if n.notified(userID, serieID, kind, ref) {
		return
	}
	note.URL = leagueTabURL
	note.Tag = "wordfeud-league-" + kind + "-" + ref
	payload, err := json.Marshal(note)
	if err != nil {
		return
	}
	if err := n.Send(db, userID, payload); err != nil {
		log.Printf("wordfeud league: push %s to user %d: %v", kind, userID, err)
		return
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO wordfeud_league_notified (user_id, serie_id, kind, ref, sent_at) VALUES (?, ?, ?, ?, ?)`,
		userID, serieID, kind, ref, n.League.Now().UTC().Format(time.RFC3339)); err != nil {
		log.Printf("wordfeud league: record notified for user %d: %v", userID, err)
	}
}

// leagueSend pushes via push.SendToUser and succeeds only when at least one
// subscription accepted it.
func leagueSend(db *sql.DB, userID int64, payload []byte) error {
	results, err := push.SendToUser(db, push.DefaultHTTPClient, userID, payload)
	if err != nil {
		return err
	}
	for _, r := range results {
		if r.Err == nil && r.StatusCode >= 200 && r.StatusCode < 300 {
			return nil
		}
	}
	return fmt.Errorf("no subscription accepted the push (%d attempted)", len(results))
}

func leagueSeasonText(lang string, season, division, opponents int) push.Notification {
	switch lang {
	case "nb":
		return push.Notification{
			Title: fmt.Sprintf("Wordfeud-ligaen: sesong %d er i gang", season),
			Body:  fmt.Sprintf("Du spiller i %d. divisjon mot %d motstandere. Trykk for å invitere dem.", division, opponents),
		}
	case "th":
		return push.Notification{
			Title: fmt.Sprintf("ลีก Wordfeud: ฤดูกาล %d เริ่มแล้ว", season),
			Body:  fmt.Sprintf("คุณอยู่ดิวิชั่น %d พบคู่แข่ง %d คน แตะเพื่อส่งคำเชิญ", division, opponents),
		}
	}
	return push.Notification{
		Title: fmt.Sprintf("Wordfeud league: season %d has started", season),
		Body:  fmt.Sprintf("You are in division %d with %d opponents. Tap to invite them.", division, opponents),
	}
}

func leagueFinishedText(lang, opponent string, my, opp int) push.Notification {
	switch lang {
	case "nb":
		return push.Notification{
			Title: fmt.Sprintf("Ligakamp mot %s er ferdig", opponent),
			Body:  fmt.Sprintf("%d–%d. Trykk for å registrere resultatet.", my, opp),
		}
	case "th":
		return push.Notification{
			Title: fmt.Sprintf("เกมลีกกับ %s จบแล้ว", opponent),
			Body:  fmt.Sprintf("%d–%d แตะเพื่อบันทึกผล", my, opp),
		}
	}
	return push.Notification{
		Title: fmt.Sprintf("League game against %s is finished", opponent),
		Body:  fmt.Sprintf("%d–%d. Tap to register the result.", my, opp),
	}
}
