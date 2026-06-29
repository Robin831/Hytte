package familychat

import (
	"database/sql"
	"errors"
	"time"
)

// Call lifecycle status values. The state machine is intentionally tiny:
// ringing → answered → ended, or ringing → missed when the recipient never
// picked up before the initiator hung up.
const (
	CallStatusRinging  = "ringing"
	CallStatusAnswered = "answered"
	CallStatusEnded    = "ended"
	CallStatusMissed   = "missed"
)

// Call kinds. 'voice' is the legacy default for audio-only calls; 'video'
// (Hytte-hob4) is the same WebRTC pipeline with a video track added so the
// conversation log can pick the right icon.
const (
	CallKindVoice = "voice"
	CallKindVideo = "video"
)

// NormalizeCallKind clamps an arbitrary input to a recognised kind. Anything
// other than the explicit 'video' value falls back to 'voice' so a malformed
// client payload never produces an unrecognised value in the database.
func NormalizeCallKind(kind string) string {
	if kind == CallKindVideo {
		return CallKindVideo
	}
	return CallKindVoice
}

// ErrCallNotFound is returned by call repository helpers when no row matches
// the (conversation_id, call_id) pair. callAnswerHandler and callEndHandler both
// map it to 404; callICEHandler does not consult the DB and never returns it.
var ErrCallNotFound = errors.New("familychat: call not found")

// InsertCall records a new call envelope for an outgoing offer. The call_id is
// supplied by the client (typically a UUID) so the same identifier can route
// every subsequent relay (answer/ice/end) without an extra round-trip. Duplicate
// offers for the same (conversation, call_id) pair are silently ignored via
// INSERT OR IGNORE; the caller treats the relay as idempotent. kind is
// clamped to a recognised value so malformed input falls back to voice.
func InsertCall(db *sql.DB, convID, initiatorUserID int64, callID, kind string) error {
	now := time.Now().UTC().Format(timeFormat)
	_, err := db.Exec(
		`INSERT OR IGNORE INTO family_chat_calls
			(conversation_id, initiator_user_id, call_id, started_at, status, kind)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		convID, initiatorUserID, callID, now, CallStatusRinging, NormalizeCallKind(kind),
	)
	return err
}

// MarkAnswered flips a ringing call to answered. Returns ErrCallNotFound when
// no row exists for (convID, callID) so the handler can refuse to relay
// phantom answer events for call_ids that never had a corresponding offer.
// When the row exists but is no longer in 'ringing' state (already answered,
// ended, or missed) the call is a no-op and returns nil — keeping the relay
// race-safe: a late answer that arrives after the initiator has hung up does
// not resurrect the call.
func MarkAnswered(db *sql.DB, convID int64, callID string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var existing string
	err = tx.QueryRow(
		`SELECT status FROM family_chat_calls WHERE conversation_id = ? AND call_id = ?`,
		convID, callID,
	).Scan(&existing)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCallNotFound
		}
		return err
	}
	if existing != CallStatusRinging {
		return tx.Commit()
	}
	if _, err := tx.Exec(
		`UPDATE family_chat_calls SET status = ? WHERE conversation_id = ? AND call_id = ?`,
		CallStatusAnswered, convID, callID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkEnded finalises a call. If the call was never answered (status is still
// 'ringing'), the row transitions to 'missed' so the conversation history can
// surface a missed-call entry. Already-ended calls are left alone.
func MarkEnded(db *sql.DB, convID int64, callID string) (status string, err error) {
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	var existing string
	err = tx.QueryRow(
		`SELECT status FROM family_chat_calls WHERE conversation_id = ? AND call_id = ?`,
		convID, callID,
	).Scan(&existing)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrCallNotFound
		}
		return "", err
	}

	// Already terminal — return the prior status untouched so the relay is idempotent.
	if existing == CallStatusEnded || existing == CallStatusMissed {
		return existing, tx.Commit()
	}

	newStatus := CallStatusEnded
	if existing == CallStatusRinging {
		// The callee never picked up before the initiator hung up.
		newStatus = CallStatusMissed
	}
	now := time.Now().UTC().Format(timeFormat)
	if _, err := tx.Exec(
		`UPDATE family_chat_calls
		    SET status = ?, ended_at = ?
		  WHERE conversation_id = ? AND call_id = ?`,
		newStatus, now, convID, callID,
	); err != nil {
		return "", err
	}
	return newStatus, tx.Commit()
}

// GetCallStatus returns the current status string for the call identified by
// (convID, callID). Used by tests; production handlers never read back the
// status — they let the lifecycle helpers above keep the row in sync.
func GetCallStatus(db *sql.DB, convID int64, callID string) (string, error) {
	var status string
	err := db.QueryRow(
		`SELECT status FROM family_chat_calls WHERE conversation_id = ? AND call_id = ?`,
		convID, callID,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrCallNotFound
	}
	return status, err
}

// JoinCall records that userID is a participant in the group call identified by
// (convID, callID) and returns the user IDs of every *other* member already in
// the call (those whose left_at is NULL). It is the group-call counterpart to
// InsertCall: the first joiner becomes the call's initiator and the row starts
// in 'ringing'; once at least two participants are active the call flips to
// 'answered'. Re-joining (e.g. after a transient disconnect) clears a prior
// left_at so the participant counts as active again.
//
// The returned list lets the joining client know which peers to dial in the
// WebRTC mesh. Pairwise glare is resolved client-side (the lower user ID makes
// the offer), so this helper does not impose any ordering beyond ascending IDs.
func JoinCall(db *sql.DB, convID, userID int64, callID, kind string) ([]int64, error) {
	now := time.Now().UTC().Format(timeFormat)

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Ensure the call envelope exists. The first joiner is recorded as the
	// initiator; subsequent joins are ignored by the UNIQUE constraint.
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO family_chat_calls
			(conversation_id, initiator_user_id, call_id, started_at, status, kind)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		convID, userID, callID, now, CallStatusRinging, NormalizeCallKind(kind),
	); err != nil {
		return nil, err
	}

	// Collect the existing active participants before inserting ourselves so the
	// caller does not see its own id in the peer list.
	others, err := activeParticipantsTx(tx, convID, callID, userID)
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(
		`INSERT INTO family_chat_call_participants
			(conversation_id, call_id, user_id, joined_at, left_at)
		 VALUES (?, ?, ?, ?, NULL)
		 ON CONFLICT(conversation_id, call_id, user_id)
		 DO UPDATE SET joined_at = excluded.joined_at, left_at = NULL`,
		convID, callID, userID, now,
	); err != nil {
		return nil, err
	}

	// Two or more active participants means the call has connected — flip the
	// envelope to 'answered' so it is not later recorded as a missed call. The
	// update is a no-op once the row has already left the 'ringing' state.
	if len(others) >= 1 {
		if _, err := tx.Exec(
			`UPDATE family_chat_calls SET status = ?
			  WHERE conversation_id = ? AND call_id = ? AND status = ?`,
			CallStatusAnswered, convID, callID, CallStatusRinging,
		); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return others, nil
}

// LeaveCall marks userID as having left the group call and returns the number of
// participants still active afterwards along with the call's final status when
// the room has emptied. When the last participant leaves, the call envelope is
// finalised via MarkEnded (transitioning to 'ended', or 'missed' if it was never
// answered) and finalStatus carries that value; while others remain, remaining
// is the live count and finalStatus is empty.
func LeaveCall(db *sql.DB, convID, userID int64, callID string) (remaining int, finalStatus string, err error) {
	now := time.Now().UTC().Format(timeFormat)
	if _, err = db.Exec(
		`UPDATE family_chat_call_participants SET left_at = ?
		  WHERE conversation_id = ? AND call_id = ? AND user_id = ? AND left_at IS NULL`,
		now, convID, callID, userID,
	); err != nil {
		return 0, "", err
	}

	remaining, err = CountActiveParticipants(db, convID, callID)
	if err != nil {
		return 0, "", err
	}
	if remaining > 0 {
		return remaining, "", nil
	}

	// Room is empty — finalise the envelope. ErrCallNotFound is treated as a
	// no-op: a leave for a call that was never recorded should not error.
	finalStatus, err = MarkEnded(db, convID, callID)
	if errors.Is(err, ErrCallNotFound) {
		return 0, "", nil
	}
	return 0, finalStatus, err
}

// ActiveParticipants returns the user IDs (ascending) currently in the call,
// i.e. those rows with left_at IS NULL.
func ActiveParticipants(db *sql.DB, convID int64, callID string) ([]int64, error) {
	return activeParticipantsTx(db, convID, callID, 0)
}

// CountActiveParticipants returns how many members are currently in the call.
func CountActiveParticipants(db *sql.DB, convID int64, callID string) (int, error) {
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM family_chat_call_participants
		  WHERE conversation_id = ? AND call_id = ? AND left_at IS NULL`,
		convID, callID,
	).Scan(&n)
	return n, err
}

// rowQuerier is satisfied by both *sql.DB and *sql.Tx so activeParticipantsTx
// can run inside or outside a transaction.
type rowQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// activeParticipantsTx lists active participant user IDs, optionally excluding
// excludeID (pass 0 to exclude nobody).
func activeParticipantsTx(q rowQuerier, convID int64, callID string, excludeID int64) ([]int64, error) {
	rows, err := q.Query(
		`SELECT user_id FROM family_chat_call_participants
		  WHERE conversation_id = ? AND call_id = ? AND left_at IS NULL
		  ORDER BY user_id`,
		convID, callID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []int64{}
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		if uid == excludeID {
			continue
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}

// GetCallKind returns the persisted kind ('voice' or 'video') for a call.
// Used by tests verifying that the offer handler plumbed the kind through.
func GetCallKind(db *sql.DB, convID int64, callID string) (string, error) {
	var kind string
	err := db.QueryRow(
		`SELECT kind FROM family_chat_calls WHERE conversation_id = ? AND call_id = ?`,
		convID, callID,
	).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrCallNotFound
	}
	return kind, err
}
