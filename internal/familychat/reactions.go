package familychat

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// ErrInvalidEmoji is returned by validateEmoji when the input is not a single
// emoji grapheme cluster or an allow-listed `:shortcode:`. Handlers map this
// to a 400 response so clients see a clear error rather than a generic
// "bad request".
var ErrInvalidEmoji = errors.New("familychat: invalid emoji")

// maxEmojiBytes caps the byte length we accept for a single emoji value.
// Compound emoji with skin tones, ZWJ sequences and variation selectors stay
// well under this; longer inputs are almost certainly spam.
const maxEmojiBytes = 64

// maxReactionUsers caps the per-emoji user list returned to clients so the
// /messages response stays bounded for large conversations. Anything past the
// cap is summarized in extra_count.
const maxReactionUsers = 20

// allowedShortcodes is the explicit set of `:shortcode:` aliases the API
// accepts. Keeping the list short avoids the need to ship a full emoji
// database in the binary while still letting clients that prefer textual
// shortcodes (legacy keyboards, accessibility tools) react to messages.
var allowedShortcodes = map[string]struct{}{
	"thumbsup": {},
	"thumbsdown": {},
	"heart": {},
	"tada": {},
	"laugh": {},
	"cry": {},
	"fire": {},
	"clap": {},
	"eyes": {},
	"smile": {},
	"sob": {},
	"angry": {},
	"thinking": {},
	"100": {},
	"pray": {},
	"rocket": {},
}

// ReactionSummary is the per-emoji bucket attached to each message in the
// list response. Users is the (possibly truncated) list of user IDs that
// reacted with the emoji; ExtraCount is the number of additional users
// not represented in Users.
type ReactionSummary struct {
	Count      int     `json:"count"`
	Users      []int64 `json:"users"`
	ExtraCount int     `json:"extra_count,omitempty"`
	Me         bool    `json:"me"`
}

// validateEmoji accepts either a single Unicode emoji grapheme cluster or a
// `:shortcode:` whose body is on the allowlist above. Anything else (empty,
// too long, multiple clusters, plain ASCII letters, etc.) is rejected.
//
// The grapheme heuristic covers:
//   - Standard emoji in the supplementary planes (👍, 🎉, etc.)
//   - Emoji with variation selector 16 (❤️)
//   - Skin-tone modifier sequences (👍🏽)
//   - ZWJ sequences (👨‍👩‍👧)
//   - Regional indicator flag pairs (🇳🇴)
//   - Keycap sequences ([0-9#*] + U+FE0F? + U+20E3, e.g. 1️⃣)
func validateEmoji(s string) error {
	if s == "" {
		return ErrInvalidEmoji
	}
	if len(s) > maxEmojiBytes {
		return ErrInvalidEmoji
	}
	if !utf8.ValidString(s) {
		return ErrInvalidEmoji
	}

	// :shortcode: path.
	if strings.HasPrefix(s, ":") && strings.HasSuffix(s, ":") && len(s) >= 3 {
		name := s[1 : len(s)-1]
		if name == "" {
			return ErrInvalidEmoji
		}
		for _, r := range name {
			if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '-' && r != '+' {
				return ErrInvalidEmoji
			}
		}
		if _, ok := allowedShortcodes[name]; !ok {
			return ErrInvalidEmoji
		}
		return nil
	}

	// Keycap sequences: [0-9#*] + optional U+FE0F (VS16) + U+20E3 (enclosing keycap).
	// These start with an ASCII rune so we handle them before the general walk.
	runes := []rune(s)
	if isKeycapBase(runes[0]) {
		if (len(runes) == 2 && runes[1] == 0x20E3) ||
			(len(runes) == 3 && runes[1] == 0xFE0F && runes[2] == 0x20E3) {
			return nil
		}
		return ErrInvalidEmoji
	}

	// Single grapheme-cluster walk. State tracks whether we just saw a ZWJ
	// (so the next emoji lead may continue a ZWJ sequence) or a regional
	// indicator (so one more regional indicator completes a flag pair).
	// A second standalone emoji lead — i.e. one that is neither a ZWJ
	// continuation nor the second half of a flag pair — is rejected so that
	// inputs like "👍👍" or "👍🎉" cannot slip through.
	first := true
	gotSymbol := false
	afterZWJ := false
	lastWasRegional := false
	consumedFlagPair := false
	for _, r := range s {
		if first {
			first = false
			if !isEmojiLead(r) {
				return ErrInvalidEmoji
			}
			gotSymbol = true
			lastWasRegional = isRegionalIndicator(r)
			continue
		}
		// Check combiner before lead: skin-tone modifiers (U+1F3FB–U+1F3FF) fall
		// inside the isEmojiLead supplementary range; treating them as combiners
		// here is the correct semantics.
		if isEmojiCombiner(r) {
			afterZWJ = r == 0x200D
			lastWasRegional = false
			continue
		}
		if isEmojiLead(r) {
			if afterZWJ {
				// ZWJ sequence continuation (e.g. 👨‍👩‍👧).
				afterZWJ = false
				lastWasRegional = isRegionalIndicator(r)
				continue
			}
			if lastWasRegional && isRegionalIndicator(r) && !consumedFlagPair {
				// Second regional indicator completes a flag pair (e.g. 🇳🇴).
				consumedFlagPair = true
				lastWasRegional = false
				continue
			}
			return ErrInvalidEmoji
		}
		return ErrInvalidEmoji
	}
	if !gotSymbol {
		return ErrInvalidEmoji
	}
	return nil
}

// isEmojiLead reports whether r is a rune that may stand alone as the start
// of an emoji grapheme. We deliberately allow common BMP symbols
// (★ ❤ ☀ etc.) and the supplementary planes where the vast majority of
// emoji live, but reject letters and digits so plain text can't slip through.
func isEmojiLead(r rune) bool {
	if r >= 0x1F300 && r <= 0x1FAFF {
		return true // misc symbols & pictographs, supplemental symbols
	}
	if r >= 0x2600 && r <= 0x27BF {
		return true // misc symbols + dingbats (includes ❤, ★, ☀)
	}
	if r >= 0x2300 && r <= 0x23FF {
		return true // misc technical (⏰ etc.)
	}
	if r >= 0x2700 && r <= 0x27BF {
		return true // dingbats (overlap above; keeps intent clear)
	}
	if r >= 0x1F1E6 && r <= 0x1F1FF {
		return true // regional indicator (flags)
	}
	if unicode.Is(unicode.So, r) || unicode.Is(unicode.Sk, r) {
		// other-symbol / modifier-symbol — catches anything we missed above.
		return true
	}
	return false
}

// isEmojiCombiner reports whether r is a rune that legitimately follows an
// emoji lead inside a single grapheme cluster (variation selectors, ZWJ,
// skin-tone modifiers, combining marks).
func isEmojiCombiner(r rune) bool {
	switch r {
	case 0x200D: // ZERO WIDTH JOINER
		return true
	case 0xFE0E, 0xFE0F: // VARIATION SELECTORS 15/16
		return true
	}
	if r >= 0x1F3FB && r <= 0x1F3FF {
		return true // skin tone modifiers
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Mc, r) {
		return true
	}
	return false
}

// isKeycapBase reports whether r is an ASCII rune that can begin a keycap
// sequence ([0-9#*] + U+FE0F? + U+20E3).
func isKeycapBase(r rune) bool {
	return (r >= '0' && r <= '9') || r == '#' || r == '*'
}

// isRegionalIndicator reports whether r is a Unicode regional indicator
// letter (U+1F1E6–U+1F1FF). Two consecutive regional indicators form a
// flag emoji (e.g. 🇳🇴 = U+1F1F3 U+1F1F4).
func isRegionalIndicator(r rune) bool {
	return r >= 0x1F1E6 && r <= 0x1F1FF
}

// reactionEventPayload is the JSON envelope broadcast over SSE for reaction
// add/remove events. It intentionally omits a `me` field: emitting per-viewer
// data would require per-subscriber payloads. Clients derive `me` by comparing
// UserID to the authenticated viewer's own ID.
type reactionEventPayload struct {
	MessageID      int64  `json:"message_id"`
	ConversationID int64  `json:"conversation_id"`
	UserID         int64  `json:"user_id"`
	Emoji          string `json:"emoji"`
	Count          int    `json:"count"`
}

// Known reaction event types. Kept as constants so callers cannot typo a name silently.
const (
	EventReactionAdded   = "reaction_added"
	EventReactionRemoved = "reaction_removed"
)

// AddReactionHandler inserts a reaction (idempotent on the (message,user,emoji)
// primary key) and broadcasts a reaction_added event. Non-members get 404.
func AddReactionHandler(db *sql.DB) http.HandlerFunc {
	return addReactionHandler(db, DefaultHub())
}

func addReactionHandler(db *sql.DB, hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convID, ok := parseConvID(r)
		if !ok {
			notFound(w)
			return
		}
		msgID, ok := parseMessageID(r)
		if !ok {
			notFound(w)
			return
		}

		var body struct {
			Emoji string `json:"emoji"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		emoji := strings.TrimSpace(body.Emoji)
		if err := validateEmoji(emoji); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid emoji"})
			return
		}

		count, changed, err := addReaction(db, convID, msgID, user.ID, emoji)
		if err != nil {
			if errors.Is(err, ErrForbidden) {
				notFound(w)
				return
			}
			log.Printf("familychat: add reaction conv=%d msg=%d user=%d: %v", convID, msgID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to add reaction"})
			return
		}

		if hub != nil && changed {
			hub.Publish(convID, Event{
				Type: EventReactionAdded,
				Data: reactionEventPayload{
					MessageID:      msgID,
					ConversationID: convID,
					UserID:         user.ID,
					Emoji:          emoji,
					Count:          count,
				},
			})
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// RemoveReactionHandler removes the requesting user's reaction with the given
// emoji and broadcasts a reaction_removed event. Non-members get 404.
func RemoveReactionHandler(db *sql.DB) http.HandlerFunc {
	return removeReactionHandler(db, DefaultHub())
}

func removeReactionHandler(db *sql.DB, hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convID, ok := parseConvID(r)
		if !ok {
			notFound(w)
			return
		}
		msgID, ok := parseMessageID(r)
		if !ok {
			notFound(w)
			return
		}

		emoji := strings.TrimSpace(r.URL.Query().Get("emoji"))
		if err := validateEmoji(emoji); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid emoji"})
			return
		}

		count, changed, err := removeReaction(db, convID, msgID, user.ID, emoji)
		if err != nil {
			if errors.Is(err, ErrForbidden) {
				notFound(w)
				return
			}
			log.Printf("familychat: remove reaction conv=%d msg=%d user=%d: %v", convID, msgID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to remove reaction"})
			return
		}

		if hub != nil && changed {
			hub.Publish(convID, Event{
				Type: EventReactionRemoved,
				Data: reactionEventPayload{
					MessageID:      msgID,
					ConversationID: convID,
					UserID:         user.ID,
					Emoji:          emoji,
					Count:          count,
				},
			})
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// addReaction validates membership + message ownership of the conversation,
// inserts the reaction row (ignoring PK conflicts), and returns the new count
// for that emoji on that message plus a boolean indicating whether the row was
// newly inserted (false means the reaction already existed — a no-op).
func addReaction(db *sql.DB, convID, msgID, userID int64, emoji string) (int, bool, error) {
	if err := requireMessageInConversation(db, convID, msgID, userID); err != nil {
		return 0, false, err
	}
	now := time.Now().UTC().Format(timeFormat)
	res, err := db.Exec(
		`INSERT INTO family_chat_message_reactions (message_id, user_id, emoji, reacted_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(message_id, user_id, emoji) DO NOTHING`,
		msgID, userID, emoji, now,
	)
	if err != nil {
		return 0, false, fmt.Errorf("insert reaction: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	count, err := reactionCount(db, msgID, emoji)
	return count, n > 0, err
}

// removeReaction validates membership + message ownership of the conversation,
// deletes the (user, emoji) row, and returns the new count for that emoji plus
// a boolean indicating whether a row was actually deleted. Removing a reaction
// that does not exist is not an error (returns count 0, changed false).
func removeReaction(db *sql.DB, convID, msgID, userID int64, emoji string) (int, bool, error) {
	if err := requireMessageInConversation(db, convID, msgID, userID); err != nil {
		return 0, false, err
	}
	res, err := db.Exec(
		`DELETE FROM family_chat_message_reactions
		 WHERE message_id = ? AND user_id = ? AND emoji = ?`,
		msgID, userID, emoji,
	)
	if err != nil {
		return 0, false, fmt.Errorf("delete reaction: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	count, err := reactionCount(db, msgID, emoji)
	return count, n > 0, err
}

// requireMessageInConversation returns ErrForbidden when userID is not a
// member of convID, or when msgID does not belong to convID. Hides
// existence: an attacker probing for a foreign message id cannot tell
// "not a member" apart from "not in this conversation".
func requireMessageInConversation(db *sql.DB, convID, msgID, userID int64) error {
	ok, err := IsMember(db, convID, userID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	var convInMsg int64
	err = db.QueryRow(
		`SELECT conversation_id FROM family_chat_messages WHERE id = ?`,
		msgID,
	).Scan(&convInMsg)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrForbidden
		}
		return err
	}
	if convInMsg != convID {
		return ErrForbidden
	}
	return nil
}

func reactionCount(db *sql.DB, msgID int64, emoji string) (int, error) {
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM family_chat_message_reactions WHERE message_id = ? AND emoji = ?`,
		msgID, emoji,
	).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// batchReactions fetches every reaction for the given message ids in a
// single query and returns them keyed by message id then emoji. The user
// list for each emoji is capped at maxReactionUsers; anything past the cap
// is rolled into extra_count. The me flag is set when meUserID appears in
// the user list (even if they are past the cap).
func batchReactions(db *sql.DB, msgIDs []int64, meUserID int64) (map[int64]map[string]*ReactionSummary, error) {
	result := make(map[int64]map[string]*ReactionSummary, len(msgIDs))
	if len(msgIDs) == 0 {
		return result, nil
	}
	placeholders := strings.Repeat("?,", len(msgIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(msgIDs))
	for i, id := range msgIDs {
		args[i] = id
	}
	rows, err := db.Query(
		`SELECT message_id, emoji, user_id
		 FROM family_chat_message_reactions
		 WHERE message_id IN (`+placeholders+`)
		 ORDER BY message_id, emoji, reacted_at, user_id`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var msgID, userID int64
		var emoji string
		if err := rows.Scan(&msgID, &emoji, &userID); err != nil {
			return nil, err
		}
		byEmoji, ok := result[msgID]
		if !ok {
			byEmoji = make(map[string]*ReactionSummary)
			result[msgID] = byEmoji
		}
		sum, ok := byEmoji[emoji]
		if !ok {
			sum = &ReactionSummary{Users: []int64{}}
			byEmoji[emoji] = sum
		}
		sum.Count++
		if userID == meUserID {
			sum.Me = true
		}
		if len(sum.Users) < maxReactionUsers {
			sum.Users = append(sum.Users, userID)
		} else {
			sum.ExtraCount++
		}
	}
	return result, rows.Err()
}

// parseMessageID extracts and validates the {messageID} URL parameter. 404
// on failure for the same reason as parseConvID — don't leak existence.
func parseMessageID(r *http.Request) (int64, bool) {
	val := chi.URLParam(r, "messageID")
	if val == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(val, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
