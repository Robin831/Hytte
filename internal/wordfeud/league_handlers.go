package wordfeud

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/wloh"
)

// maxLeagueScore bounds a reported score; no Wordfeud game gets near it.
const maxLeagueScore = 2000

// writeLeagueError maps league-site failures to a response. code is a stable
// key the frontend translates.
func writeLeagueError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, wloh.ErrPlayerNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "player not found in a current league group", "code": "player_not_found"})
	case errors.Is(err, wloh.ErrLoginFailed):
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "the league site rejected the username and email", "code": "league_login_failed"})
	default:
		log.Printf("wordfeud league: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "the league site could not be reached", "code": "league_unavailable"})
	}
}

// LeagueHandler returns the user's league group and matches.
// GET /api/wordfeud/league[?refresh=1]
func LeagueHandler(l *League) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		view, err := l.View(r.Context(), user.ID, r.URL.Query().Get("refresh") != "")
		if err != nil {
			writeLeagueError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// LeagueConfigHandler stores the user's league username and email. The email
// is optional and only needed to post results; when given it is checked
// against the league site. An empty username removes the league setup.
// PUT /api/wordfeud/league/config
func LeagueConfigHandler(l *League) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
		var body struct {
			Username string `json:"username"`
			Email    string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		username, email := strings.TrimSpace(body.Username), strings.TrimSpace(body.Email)
		if len(username) > 60 || len(email) > 200 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username or email too long"})
			return
		}

		if username == "" {
			if _, err := l.DB.Exec(`DELETE FROM user_preferences WHERE user_id = ? AND key IN (?, ?)`, user.ID, PrefLeagueUsername, PrefLeagueEmail); err != nil {
				log.Printf("wordfeud league: clear config for user %d: %v", user.ID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save league settings"})
				return
			}
			if _, err := l.DB.Exec(`DELETE FROM wordfeud_league WHERE user_id = ?`, user.ID); err != nil {
				log.Printf("wordfeud league: clear group for user %d: %v", user.ID, err)
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
			return
		}

		// Confirm the player has a group before saving, so a typo surfaces now.
		if _, err := l.Site.FindGroup(r.Context(), username); err != nil {
			writeLeagueError(w, err)
			return
		}
		if email == "" {
			// Editing without retyping the email keeps the one already stored.
			if prev, err := loadLeagueConfig(l.DB, user.ID); err == nil && strings.EqualFold(prev.username, username) {
				email = prev.email
			}
		} else {
			if err := l.Site.CheckLogin(r.Context(), username, email); err != nil {
				writeLeagueError(w, err)
				return
			}
		}

		encUser, err := encryption.EncryptField(username)
		if err == nil {
			err = auth.SetPreference(l.DB, user.ID, PrefLeagueUsername, encUser)
		}
		if err == nil {
			var encEmail string
			if encEmail, err = encryption.EncryptField(email); err == nil {
				err = auth.SetPreference(l.DB, user.ID, PrefLeagueEmail, encEmail)
			}
		}
		if err != nil {
			log.Printf("wordfeud league: save config for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save league settings"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
	}
}

// leagueMatchFor returns the user's current match against opponent.
func leagueMatchFor(view *LeagueView, opponent string) *LeagueMatch {
	for i := range view.Matches {
		if strings.EqualFold(view.Matches[i].Opponent, opponent) {
			return &view.Matches[i]
		}
	}
	return nil
}

// LeagueInviteHandler invites a league opponent to a Wordfeud game. Only
// opponents in the user's group with no game or invitation yet can be invited.
// POST /api/wordfeud/league/invite
func LeagueInviteHandler(l *League) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
		var body struct {
			Opponent string `json:"opponent"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Opponent) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "opponent is required"})
			return
		}

		view, err := l.View(r.Context(), user.ID, false)
		if err != nil {
			writeLeagueError(w, err)
			return
		}
		if !view.Configured || view.Group == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "league is not set up", "code": "not_configured"})
			return
		}
		if !view.WordfeudConnected {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no Wordfeud account connected", "code": "not_connected"})
			return
		}
		ruleset := view.inviteRuleset
		if !view.CanInvite {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invites are not supported for this league", "code": "invite_unsupported"})
			return
		}
		match := leagueMatchFor(view, body.Opponent)
		if match == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "opponent is not in your league group", "code": "not_in_group"})
			return
		}
		if match.Status != MatchNotStarted {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "there is already a game or invitation with this opponent", "code": "already_started"})
			return
		}

		err = withSession(l.DB, l.WF, user.ID, func(token string) error {
			return l.WF.Invite(token, match.Opponent, ruleset, BoardNormal)
		})
		var apiErr *APIError
		switch {
		case err == nil:
			writeJSON(w, http.StatusOK, map[string]string{"status": "invited", "opponent": match.Opponent})
		case errors.As(err, &apiErr):
			// Wordfeud refused the invitation; pass its reason on as-is.
			log.Printf("wordfeud league: invite %q for user %d refused: %v", match.Opponent, user.ID, err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Wordfeud refused the invitation", "code": "invite_refused", "detail": strings.TrimSpace(apiErr.Type + " " + apiErr.Message)})
		default:
			log.Printf("wordfeud league: invite %q for user %d failed: %v", match.Opponent, user.ID, err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to send the invitation", "code": "invite_failed"})
		}
	}
}

// LeagueReportHandler registers a result on the league site. The scores come
// from the user, who has confirmed them in the UI.
// POST /api/wordfeud/league/report
func LeagueReportHandler(l *League) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
		var body struct {
			Opponent string `json:"opponent"`
			MyScore  *int   `json:"my_score"`
			OppScore *int   `json:"opp_score"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.MyScore == nil || body.OppScore == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "opponent, my_score and opp_score are required"})
			return
		}
		my, opp := *body.MyScore, *body.OppScore
		if my < 0 || opp < 0 || my > maxLeagueScore || opp > maxLeagueScore {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scores are out of range"})
			return
		}

		cfg, err := loadLeagueConfig(l.DB, user.ID)
		if err != nil {
			log.Printf("wordfeud league: read config for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read league settings"})
			return
		}
		if cfg.username == "" || cfg.email == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "add your league email to post results", "code": "not_configured"})
			return
		}
		g, _, _, err := l.group(r.Context(), user.ID, cfg.username, leagueViewMaxAge)
		if err != nil {
			writeLeagueError(w, err)
			return
		}
		opponent := g.Player(body.Opponent)
		if opponent == "" || strings.EqualFold(opponent, cfg.username) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "opponent is not in your league group", "code": "not_in_group"})
			return
		}
		if _, _, done := g.Result(cfg.username, opponent); done {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "this match already has a result", "code": "already_reported"})
			return
		}

		after, err := l.Site.SubmitResult(r.Context(), cfg.username, cfg.email, g.SerieID, opponent, my, opp)
		if after != nil {
			if saveErr := saveLeagueSnapshot(l.DB, user.ID, after, l.Now()); saveErr != nil {
				log.Printf("wordfeud league: store group for user %d: %v", user.ID, saveErr)
			}
		}
		var resErr *wloh.ResultError
		switch {
		case err == nil:
			log.Printf("wordfeud league: user %d registered %d-%d against %s in serie %d", user.ID, my, opp, opponent, g.SerieID)
			writeJSON(w, http.StatusOK, map[string]string{"status": "reported"})
		case errors.As(err, &resErr):
			log.Printf("wordfeud league: result for user %d against %s did not register: %v", user.ID, opponent, err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "the league site did not register the result", "code": "not_registered", "detail": resErr.Notice})
		default:
			writeLeagueError(w, err)
		}
	}
}
