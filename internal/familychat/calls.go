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

// ErrCallNotFound is returned by call repository helpers when no row matches
// the (conversation_id, call_id) pair. Handlers map this to 404 (offer/end) or
// 200-with-noop semantics (answer/ice race conditions) as appropriate.
var ErrCallNotFound = errors.New("familychat: call not found")

// InsertCall records a new call envelope for an outgoing offer. The call_id is
// supplied by the client (typically a UUID) so the same identifier can route
// every subsequent relay (answer/ice/end) without an extra round-trip. Returns
// ErrCallNotFound only on UNIQUE collisions — duplicate offers for the same
// (conversation, call_id) pair return nil; the caller treats the relay as
// idempotent.
func InsertCall(db *sql.DB, convID, initiatorUserID int64, callID string) error {
	now := time.Now().UTC().Format(timeFormat)
	_, err := db.Exec(
		`INSERT OR IGNORE INTO family_chat_calls
			(conversation_id, initiator_user_id, call_id, started_at, status)
		 VALUES (?, ?, ?, ?, ?)`,
		convID, initiatorUserID, callID, now, CallStatusRinging,
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
