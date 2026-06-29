package familychat

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/go-chi/chi/v5"
)

// maxCallIDLen caps the client-supplied call identifier. UUIDv4 is 36 chars
// with hyphens; 128 leaves headroom for any other client convention while
// keeping URL paths bounded.
const maxCallIDLen = 128

// maxSignalPayloadLen caps the opaque SDP / ICE candidate payload the relay
// forwards. WebRTC SDP for a single peer is typically ~3-8 KiB; 64 KiB leaves
// generous room for video codecs while preventing one client from using the
// signalling channel to bulk-transfer data through the hub.
const maxSignalPayloadLen = 64 * 1024

// parseCallParams extracts the conversation id and client-supplied call id
// from the URL. Returns false (with a 404 response already written) for any
// invalid input so the API does not leak the existence of conversations the
// caller cannot read.
func parseCallParams(w http.ResponseWriter, r *http.Request) (int64, string, bool) {
	convID, ok := parseConvID(r)
	if !ok {
		notFound(w)
		return 0, "", false
	}
	callID := strings.TrimSpace(chi.URLParam(r, "call_id"))
	if callID == "" || len(callID) > maxCallIDLen {
		notFound(w)
		return 0, "", false
	}
	return convID, callID, true
}

// requireCallMember verifies the requesting user belongs to convID. It
// responds with 404 for non-members and 500 for genuine DB failures, matching
// the existing handler conventions for member-gated endpoints.
func requireCallMember(w http.ResponseWriter, db *sql.DB, userID, convID int64) bool {
	ok, err := IsMember(db, convID, userID)
	if err != nil {
		log.Printf("familychat: calls membership conv=%d user=%d: %v", convID, userID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return false
	}
	if !ok {
		notFound(w)
		return false
	}
	return true
}

// readSignalBody pulls the opaque relay body off the wire. The handler never
// inspects the JSON contents — `data` is whatever the client posted (SDP for
// offer/answer, an ICE candidate object for ice, or an arbitrary blob for
// end). Using json.RawMessage avoids decoding cost and keeps the relay
// forward-compatible with future WebRTC payload shapes. A nil/empty body is
// allowed for the end handler so a "hang up" request need not carry data.
func readSignalBody(w http.ResponseWriter, r *http.Request, allowEmpty bool) (json.RawMessage, int64, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSignalPayloadLen)
	defer r.Body.Close()
	var body struct {
		Data json.RawMessage `json:"data"`
		// To addresses the relay at a single peer in a group-call mesh. Zero
		// (the default / absent) means "broadcast to every member", which is the
		// 1:1 behaviour. Receivers ignore a frame whose to_user_id is set and is
		// not their own id.
		To int64 `json:"to_user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		if allowEmpty && errors.Is(err, io.EOF) {
			// Treat a genuinely empty body as no payload — useful for /end.
			// Any other error (malformed JSON, body-too-large) still returns 400.
			return nil, 0, true
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return nil, 0, false
	}
	if len(body.Data) == 0 && !allowEmpty {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "data is required"})
		return nil, 0, false
	}
	return body.Data, body.To, true
}

// readOfferBody extends readSignalBody for the offer endpoint, which also
// carries a top-level `kind` field ('voice'|'video'). The kind is optional
// so older clients that pre-date video support continue to ring as voice.
func readOfferBody(w http.ResponseWriter, r *http.Request) (data json.RawMessage, kind string, to int64, ok bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSignalPayloadLen)
	defer r.Body.Close()
	var body struct {
		Data json.RawMessage `json:"data"`
		Kind string          `json:"kind"`
		// To is set when this offer is one leg of a group-call mesh, addressed
		// to a single peer. When set the handler treats the offer as a pure
		// relay (no call-envelope insert, no webpush wake-up) — the call lifecycle
		// for group calls is driven by the join/leave endpoints instead.
		To int64 `json:"to_user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return nil, "", 0, false
	}
	if len(body.Data) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "data is required"})
		return nil, "", 0, false
	}
	return body.Data, NormalizeCallKind(body.Kind), body.To, true
}

// callRelayPayload builds the SSE event payload for a single relay hop. The
// shape is identical across event types so the frontend's signalling layer
// can route by event name alone.
func callRelayPayload(convID, fromUserID int64, callID string, data json.RawMessage) map[string]any {
	return map[string]any{
		"conversation_id": convID,
		"call_id":         callID,
		"from_user_id":    fromUserID,
		"data":            data,
	}
}

// withTarget annotates a relay payload with the addressed peer for group-call
// mesh signalling. A zero target is left off entirely so 1:1 frames keep their
// original (untargeted) shape and existing receivers are unaffected.
func withTarget(payload map[string]any, toUserID int64) map[string]any {
	if toUserID != 0 {
		payload["to_user_id"] = toUserID
	}
	return payload
}

// callOfferRelayPayload extends callRelayPayload with the call kind so the
// receiver can branch on voice vs video before fetching media (only the offer
// frame needs this; the kind sticks to the local call state from then on).
func callOfferRelayPayload(convID, fromUserID int64, callID, kind string, data json.RawMessage) map[string]any {
	payload := callRelayPayload(convID, fromUserID, callID, data)
	payload["kind"] = NormalizeCallKind(kind)
	return payload
}

// CallOfferHandler relays a WebRTC offer through the SSE hub and records the
// call envelope so missed-call history works. It also fans out a high-priority
// webpush notification to every conversation member that is not currently on
// SSE — that is what makes an unattended phone ring.
func CallOfferHandler(db *sql.DB) http.HandlerFunc {
	return callOfferHandler(db, DefaultHub(), defaultPushSender(db), false /* notify async */)
}

func callOfferHandler(db *sql.DB, hub *Hub, sender PushSenderFunc, notifySync bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convID, callID, ok := parseCallParams(w, r)
		if !ok {
			return
		}
		if !requireCallMember(w, db, user.ID, convID) {
			return
		}

		data, kind, to, ok := readOfferBody(w, r)
		if !ok {
			return
		}

		// A targeted offer (to_user_id set) is one leg of a group-call mesh: the
		// call envelope and the webpush wake-up are owned by the join endpoint,
		// so here we relay the SDP to the addressed peer and nothing else. An
		// untargeted offer is the legacy 1:1 flow that records the call and rings
		// the recipients.
		if to == 0 {
			if err := InsertCall(db, convID, user.ID, callID, kind); err != nil {
				log.Printf("familychat: insert call conv=%d call=%s user=%d: %v", convID, callID, user.ID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to record call"})
				return
			}
		}

		if hub != nil {
			hub.Publish(convID, Event{
				Type: EventCallOffer,
				Data: withTarget(callOfferRelayPayload(convID, user.ID, callID, kind, data), to),
			})
		}

		// Webpush wake-up — only to recipients who aren't already streaming
		// the conversation. The same gating used by message delivery applies:
		// a phone that already shows the chat doesn't need a banner. Targeted
		// (group) offers skip this — the join endpoint already woke everyone.
		if sender != nil && to == 0 {
			senderName := senderDisplayName(user)
			fire := func() {
				notifyCallRecipients(db, hub, sender, convID, user.ID, callID, senderName)
			}
			if notifySync {
				fire()
			} else {
				go func() {
					pushSem <- struct{}{}
					defer func() { <-pushSem }()
					fire()
				}()
			}
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// CallAnswerHandler relays a WebRTC answer and marks the call answered.
func CallAnswerHandler(db *sql.DB) http.HandlerFunc {
	return callAnswerHandler(db, DefaultHub())
}

func callAnswerHandler(db *sql.DB, hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convID, callID, ok := parseCallParams(w, r)
		if !ok {
			return
		}
		if !requireCallMember(w, db, user.ID, convID) {
			return
		}

		data, to, ok := readSignalBody(w, r, false)
		if !ok {
			return
		}

		if err := MarkAnswered(db, convID, callID); err != nil {
			if errors.Is(err, ErrCallNotFound) {
				// Refuse to relay phantom answer events for call_ids that were
				// never offered — otherwise any member could spam fake
				// call_answer events at the rest of the conversation.
				notFound(w)
				return
			}
			log.Printf("familychat: mark answered conv=%d call=%s: %v", convID, callID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update call"})
			return
		}

		if hub != nil {
			hub.Publish(convID, Event{
				Type: EventCallAnswer,
				Data: withTarget(callRelayPayload(convID, user.ID, callID, data), to),
			})
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// CallICEHandler relays a single ICE candidate. ICE trickling can fire several
// candidates per call, so this is the highest-volume relay endpoint. Nothing
// is persisted.
func CallICEHandler(db *sql.DB) http.HandlerFunc {
	return callICEHandler(db, DefaultHub())
}

func callICEHandler(db *sql.DB, hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convID, callID, ok := parseCallParams(w, r)
		if !ok {
			return
		}
		if !requireCallMember(w, db, user.ID, convID) {
			return
		}

		data, to, ok := readSignalBody(w, r, false)
		if !ok {
			return
		}

		if hub != nil {
			hub.Publish(convID, Event{
				Type: EventCallICE,
				Data: withTarget(callRelayPayload(convID, user.ID, callID, data), to),
			})
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// CallEndHandler relays a hang-up event and finalises the call record. If the
// call was never answered the row transitions to 'missed' so the UI can show
// a missed-call entry.
func CallEndHandler(db *sql.DB) http.HandlerFunc {
	return callEndHandler(db, DefaultHub())
}

func callEndHandler(db *sql.DB, hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convID, callID, ok := parseCallParams(w, r)
		if !ok {
			return
		}
		if !requireCallMember(w, db, user.ID, convID) {
			return
		}

		// An /end request can legitimately be empty — the client just wants to
		// hang up. allowEmpty also covers the network-hiccup case where the
		// retry handler resends with an empty body. /end is never targeted at a
		// single peer (a hang-up tears the whole 1:1 call down), so the relay
		// target is ignored here.
		data, _, ok := readSignalBody(w, r, true)
		if !ok {
			return
		}

		status, err := MarkEnded(db, convID, callID)
		if err != nil {
			if errors.Is(err, ErrCallNotFound) {
				// Refuse to relay phantom end events for call_ids that were
				// never offered. Without this, any member could fan a fake
				// call_end out to the entire conversation.
				notFound(w)
				return
			}
			log.Printf("familychat: mark ended conv=%d call=%s: %v", convID, callID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update call"})
			return
		}

		if hub != nil {
			payload := callRelayPayload(convID, user.ID, callID, data)
			// Surface the final status so clients can render the right history
			// entry (ended vs missed) without an extra query.
			if status != "" {
				payload["status"] = status
			}
			hub.Publish(convID, Event{
				Type: EventCallEnd,
				Data: payload,
			})
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// readJoinBody reads the optional `kind` for a group-call join. An empty body
// is allowed (the call kind defaults to voice) so a bare "join" POST works.
func readJoinBody(w http.ResponseWriter, r *http.Request) (string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSignalPayloadLen)
	defer r.Body.Close()
	var body struct {
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		if errors.Is(err, io.EOF) {
			return CallKindVoice, true
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return "", false
	}
	return NormalizeCallKind(body.Kind), true
}

// CallJoinHandler records the caller as a participant in a group (3+ member)
// call and returns the user IDs of the peers already in the call so the client
// can build out the WebRTC mesh. It broadcasts a call_join event to the
// conversation (existing participants dial the newcomer) and wakes offline
// members with a high-priority webpush, mirroring the 1:1 offer flow.
func CallJoinHandler(db *sql.DB) http.HandlerFunc {
	return callJoinHandler(db, DefaultHub(), defaultPushSender(db), false /* notify async */)
}

func callJoinHandler(db *sql.DB, hub *Hub, sender PushSenderFunc, notifySync bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convID, callID, ok := parseCallParams(w, r)
		if !ok {
			return
		}
		if !requireCallMember(w, db, user.ID, convID) {
			return
		}

		kind, ok := readJoinBody(w, r)
		if !ok {
			return
		}

		peers, err := JoinCall(db, convID, user.ID, callID, kind)
		if err != nil {
			log.Printf("familychat: join call conv=%d call=%s user=%d: %v", convID, callID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to join call"})
			return
		}

		if hub != nil {
			hub.Publish(convID, Event{
				Type: EventCallJoin,
				Data: map[string]any{
					"conversation_id": convID,
					"call_id":         callID,
					"from_user_id":    user.ID,
					"kind":            NormalizeCallKind(kind),
				},
			})
		}

		// Wake offline members so an unattended phone rings, the same way an
		// incoming 1:1 offer does. Only the first joiner triggers a ring — once
		// peers are present, later joins are people answering, not new calls.
		if sender != nil && len(peers) == 0 {
			senderName := senderDisplayName(user)
			fire := func() {
				notifyCallRecipientsBody(db, hub, sender, convID, user.ID, callID, senderName, "Group call")
			}
			if notifySync {
				fire()
			} else {
				go func() {
					pushSem <- struct{}{}
					defer func() { <-pushSem }()
					fire()
				}()
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"call_id":      callID,
			"kind":         NormalizeCallKind(kind),
			"participants": peers,
		})
	}
}

// CallLeaveHandler records the caller leaving a group call and relays a
// call_leave so peers tear down their single connection to them. When the last
// participant leaves, the call envelope is finalised and a call_end is relayed
// so any in-app "active group call" banner clears.
func CallLeaveHandler(db *sql.DB) http.HandlerFunc {
	return callLeaveHandler(db, DefaultHub())
}

func callLeaveHandler(db *sql.DB, hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convID, callID, ok := parseCallParams(w, r)
		if !ok {
			return
		}
		if !requireCallMember(w, db, user.ID, convID) {
			return
		}

		remaining, finalStatus, err := LeaveCall(db, convID, user.ID, callID)
		if err != nil {
			log.Printf("familychat: leave call conv=%d call=%s user=%d: %v", convID, callID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to leave call"})
			return
		}

		if hub != nil {
			hub.Publish(convID, Event{
				Type: EventCallLeave,
				Data: map[string]any{
					"conversation_id": convID,
					"call_id":         callID,
					"from_user_id":    user.ID,
				},
			})
			// Room emptied — broadcast a terminal call_end so non-participants'
			// "join group call" banners disappear and history reflects the end.
			if remaining == 0 {
				payload := map[string]any{
					"conversation_id": convID,
					"call_id":         callID,
					"from_user_id":    user.ID,
				}
				if finalStatus != "" {
					payload["status"] = finalStatus
				}
				hub.Publish(convID, Event{Type: EventCallEnd, Data: payload})
			}
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// notifyCallRecipients fans a high-priority webpush notification out to every
// conversation member except the initiator who is not already subscribed via
// SSE. The tag is keyed on conversation_id so a fresh incoming call replaces
// any stale call banner on the recipient's device rather than stacking up.
func notifyCallRecipients(db *sql.DB, hub *Hub, sender PushSenderFunc, convID, initiatorID int64, callID, initiatorName string) {
	notifyCallRecipientsBody(db, hub, sender, convID, initiatorID, callID, initiatorName, "Incoming call")
}

// notifyCallRecipientsBody is the shared fan-out used by both 1:1 calls
// ("Incoming call") and group calls ("Group call"). The body text is the only
// thing that differs; the tag stays keyed on conversation_id so a fresh ring
// replaces any stale banner rather than stacking.
func notifyCallRecipientsBody(db *sql.DB, hub *Hub, sender PushSenderFunc, convID, initiatorID int64, callID, initiatorName, body string) {
	members, err := listMemberIDs(db, convID)
	if err != nil {
		log.Printf("familychat: notify call: list members conv=%d: %v", convID, err)
		return
	}

	// callID is client-supplied; QueryEscape so values containing reserved
	// URL characters (or stray '?'/'&'/spaces) cannot break the deep link.
	note := push.Notification{
		Title:   initiatorName,
		Body:    body,
		URL:     fmt.Sprintf("/familychat/%d?call=%s", convID, url.QueryEscape(callID)),
		Tag:     fmt.Sprintf("familychat-call-%d", convID),
		Urgency: "high",
	}
	payload, err := json.Marshal(note)
	if err != nil {
		log.Printf("familychat: notify call: marshal payload conv=%d: %v", convID, err)
		return
	}

	for _, uid := range members {
		if uid == initiatorID {
			continue
		}
		if hub != nil && hub.HasSubscriber(uid, convID) {
			continue
		}
		if err := sender(uid, payload); err != nil {
			log.Printf("familychat: notify call: send to user=%d conv=%d: %v", uid, convID, err)
		}
	}
}
