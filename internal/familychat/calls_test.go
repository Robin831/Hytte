package familychat

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/go-chi/chi/v5"
)

// callsRouter wires the four relay endpoints behind a request-context user
// injector so tests can hit them like the production middleware stack would.
// notifySync forces the offer handler's webpush fan-out to run inline so the
// assertions can read the captured payloads after ServeHTTP returns.
func callsRouter(t *testing.T, db *sql.DB, hub *Hub, sender PushSenderFunc, user *auth.User, notifySync bool) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.ContextWithUser(req.Context(), user)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Post("/api/familychat/conversations/{id}/calls/{call_id}/offer", callOfferHandler(db, hub, sender, notifySync))
	r.Post("/api/familychat/conversations/{id}/calls/{call_id}/answer", callAnswerHandler(db, hub))
	r.Post("/api/familychat/conversations/{id}/calls/{call_id}/ice", callICEHandler(db, hub))
	r.Post("/api/familychat/conversations/{id}/calls/{call_id}/end", callEndHandler(db, hub))
	return r
}

func TestCallsRoundTrip_OfferAnswerEnd(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	hub := NewHub()
	bobSub := hub.Subscribe(2, convID)
	defer hub.Unsubscribe(convID, bobSub)

	sender := func(int64, []byte) error { return nil }
	srv := httptest.NewServer(callsRouter(t, db, hub, sender, &auth.User{ID: 1, Name: "Alice"}, true))
	defer srv.Close()

	callID := "call-uuid-1"
	url := func(suffix string) string {
		return fmt.Sprintf("%s/api/familychat/conversations/%d/calls/%s/%s", srv.URL, convID, callID, suffix)
	}

	// 1. Offer — record + SSE event.
	postJSON(t, url("offer"), `{"data":{"type":"offer","sdp":"v=0..."}}`)
	expectCallEvent(t, bobSub, EventCallOffer, callID)
	if got, _ := GetCallStatus(db, convID, callID); got != CallStatusRinging {
		t.Fatalf("after offer: status=%q, want %q", got, CallStatusRinging)
	}

	// 2. Answer — status flips to answered.
	postJSON(t, url("answer"), `{"data":{"type":"answer","sdp":"v=0..."}}`)
	expectCallEvent(t, bobSub, EventCallAnswer, callID)
	if got, _ := GetCallStatus(db, convID, callID); got != CallStatusAnswered {
		t.Fatalf("after answer: status=%q, want %q", got, CallStatusAnswered)
	}

	// 3. ICE — relayed without touching the record.
	postJSON(t, url("ice"), `{"data":{"candidate":"candidate:1 1 udp 2122260223 192.168.1.1 49152 typ host"}}`)
	expectCallEvent(t, bobSub, EventCallICE, callID)
	if got, _ := GetCallStatus(db, convID, callID); got != CallStatusAnswered {
		t.Fatalf("after ice: status changed unexpectedly: %q", got)
	}

	// 4. End — answered call transitions to 'ended', not 'missed'.
	postJSON(t, url("end"), `{}`)
	expectCallEvent(t, bobSub, EventCallEnd, callID)
	if got, _ := GetCallStatus(db, convID, callID); got != CallStatusEnded {
		t.Fatalf("after end: status=%q, want %q", got, CallStatusEnded)
	}
	var endedAt sql.NullString
	if err := db.QueryRow(`SELECT ended_at FROM family_chat_calls WHERE conversation_id = ? AND call_id = ?`, convID, callID).Scan(&endedAt); err != nil {
		t.Fatalf("read ended_at: %v", err)
	}
	if !endedAt.Valid || endedAt.String == "" {
		t.Errorf("ended_at not stamped: %+v", endedAt)
	}
}

func TestCallOffer_MissedWhenEndedWithoutAnswer(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	hub := NewHub()
	bobSub := hub.Subscribe(2, convID)
	defer hub.Unsubscribe(convID, bobSub)

	sender := func(int64, []byte) error { return nil }
	srv := httptest.NewServer(callsRouter(t, db, hub, sender, &auth.User{ID: 1, Name: "Alice"}, true))
	defer srv.Close()

	callID := "call-missed-1"
	postJSON(t, fmt.Sprintf("%s/api/familychat/conversations/%d/calls/%s/offer", srv.URL, convID, callID), `{"data":{"sdp":"v=0..."}}`)
	expectCallEvent(t, bobSub, EventCallOffer, callID)
	postJSON(t, fmt.Sprintf("%s/api/familychat/conversations/%d/calls/%s/end", srv.URL, convID, callID), `{}`)
	expectCallEvent(t, bobSub, EventCallEnd, callID)

	got, err := GetCallStatus(db, convID, callID)
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	if got != CallStatusMissed {
		t.Errorf("status=%q, want %q (ended without answer)", got, CallStatusMissed)
	}
}

func TestCallsHandlers_NonMember404(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	// Carol (id=3) is not a member of the alice/bob conversation.
	sender := func(int64, []byte) error { return nil }
	srv := httptest.NewServer(callsRouter(t, db, NewHub(), sender, &auth.User{ID: 3, Name: "Carol"}, true))
	defer srv.Close()

	endpoints := []string{"offer", "answer", "ice", "end"}
	for _, ep := range endpoints {
		u := fmt.Sprintf("%s/api/familychat/conversations/%d/calls/x/%s", srv.URL, convID, ep)
		resp, err := http.Post(u, "application/json", strings.NewReader(`{"data":{}}`))
		if err != nil {
			t.Fatalf("post %s: %v", ep, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("non-member %s: got %d, want 404", ep, resp.StatusCode)
		}
	}

	// No call rows should have been created by the unauthorised attempts.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM family_chat_calls`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("non-member created %d call row(s)", n)
	}
}

func TestCallOffer_WebpushSentWithHighPriorityTag(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")
	convID := seedConversation(t, db, 1, "Family", 2, 3)

	hub := NewHub()
	// Bob is live on SSE — fan-out must skip him.
	bobSub := hub.Subscribe(2, convID)
	defer hub.Unsubscribe(convID, bobSub)
	go func() {
		for range bobSub.Events() {
		}
	}()

	type captured struct {
		userID  int64
		payload []byte
	}
	var mu sync.Mutex
	var calls []captured
	sender := func(uid int64, payload []byte) error {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, captured{userID: uid, payload: append([]byte(nil), payload...)})
		return nil
	}

	srv := httptest.NewServer(callsRouter(t, db, hub, sender, &auth.User{ID: 1, Name: "Alice"}, true))
	defer srv.Close()

	callID := "ring-uuid-9"
	postJSON(t, fmt.Sprintf("%s/api/familychat/conversations/%d/calls/%s/offer", srv.URL, convID, callID), `{"data":{"sdp":"v=0..."}}`)

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 push (offline recipient only), got %d", len(calls))
	}
	if calls[0].userID != 3 {
		t.Errorf("push went to user %d, want 3 (offline)", calls[0].userID)
	}
	var note push.Notification
	if err := json.Unmarshal(calls[0].payload, &note); err != nil {
		t.Fatalf("decode push payload: %v", err)
	}
	if note.Title != "Alice" {
		t.Errorf("title=%q, want Alice", note.Title)
	}
	if want := fmt.Sprintf("familychat-call-%d", convID); note.Tag != want {
		t.Errorf("tag=%q, want %q", note.Tag, want)
	}
	if note.Urgency != "high" {
		t.Errorf("urgency=%q, want high", note.Urgency)
	}
}

func TestCallEnd_AlreadyEndedIsIdempotent(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")

	if err := InsertCall(db, convID, 1, "abc"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := MarkAnswered(db, convID, "abc"); err != nil {
		t.Fatalf("answer: %v", err)
	}
	first, err := MarkEnded(db, convID, "abc")
	if err != nil {
		t.Fatalf("first end: %v", err)
	}
	if first != CallStatusEnded {
		t.Errorf("first end status=%q, want %q", first, CallStatusEnded)
	}
	second, err := MarkEnded(db, convID, "abc")
	if err != nil {
		t.Fatalf("second end: %v", err)
	}
	if second != CallStatusEnded {
		t.Errorf("second end status=%q, want %q (idempotent)", second, CallStatusEnded)
	}
}

func TestCallOffer_DuplicateCallIDIsIdempotent(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")

	if err := InsertCall(db, convID, 1, "dup"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := InsertCall(db, convID, 1, "dup"); err != nil {
		t.Fatalf("second insert (should be no-op): %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM family_chat_calls WHERE conversation_id = ? AND call_id = ?`, convID, "dup").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("duplicate offer wrote %d rows, want 1", n)
	}
}

func TestCallAnswerAndEnd_PhantomCallReturns404(t *testing.T) {
	// /answer and /end for a call_id that was never offered must not relay
	// SSE events to the rest of the conversation — otherwise any member can
	// fan phantom call_answer / call_end events at everyone else.
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	hub := NewHub()
	bobSub := hub.Subscribe(2, convID)
	defer hub.Unsubscribe(convID, bobSub)

	sender := func(int64, []byte) error { return nil }
	srv := httptest.NewServer(callsRouter(t, db, hub, sender, &auth.User{ID: 1, Name: "Alice"}, true))
	defer srv.Close()

	for _, ep := range []string{"answer", "end"} {
		u := fmt.Sprintf("%s/api/familychat/conversations/%d/calls/no-such-call/%s", srv.URL, convID, ep)
		resp, err := http.Post(u, "application/json", strings.NewReader(`{"data":{}}`))
		if err != nil {
			t.Fatalf("post %s: %v", ep, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("phantom %s: got %d, want 404", ep, resp.StatusCode)
		}
	}

	// No SSE event should have been delivered.
	select {
	case evt := <-bobSub.Events():
		t.Errorf("phantom call leaked SSE event: %+v", evt)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestCallOffer_WebpushURLEncodesCallID(t *testing.T) {
	// call_id is client-supplied so the deep link in the webpush payload must
	// URL-encode it; otherwise a value containing reserved characters
	// (spaces, '&', etc.) would corrupt the link.
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	var captured []byte
	sender := func(_ int64, payload []byte) error {
		captured = append([]byte(nil), payload...)
		return nil
	}
	srv := httptest.NewServer(callsRouter(t, db, NewHub(), sender, &auth.User{ID: 1, Name: "Alice"}, true))
	defer srv.Close()

	// space + '&' is enough to exercise QueryEscape vs PathEscape divergence:
	// QueryEscape encodes both ('+' and '%26'), PathEscape leaves '&' alone.
	callID := "weird id&foo"
	postJSON(t,
		fmt.Sprintf("%s/api/familychat/conversations/%d/calls/%s/offer", srv.URL, convID, url.PathEscape(callID)),
		`{"data":{"sdp":"v=0..."}}`,
	)

	if captured == nil {
		t.Fatal("expected a push payload to be captured")
	}
	var note push.Notification
	if err := json.Unmarshal(captured, &note); err != nil {
		t.Fatalf("decode push payload: %v", err)
	}
	want := fmt.Sprintf("/familychat/%d?call=%s", convID, url.QueryEscape(callID))
	if note.URL != want {
		t.Errorf("URL=%q, want %q", note.URL, want)
	}
	// Defensive: the encoded URL must not contain raw control chars or '&'
	// that could be mistaken for an extra query parameter delimiter.
	if strings.Contains(note.URL[strings.Index(note.URL, "?call=")+len("?call="):], "&") {
		t.Errorf("URL %q embeds an unescaped '&' after the call= value", note.URL)
	}
}

func TestCallOffer_OversizedPayloadRejected(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")

	sender := func(int64, []byte) error { return nil }
	srv := httptest.NewServer(callsRouter(t, db, NewHub(), sender, &auth.User{ID: 1, Name: "Alice"}, true))
	defer srv.Close()

	huge := strings.Repeat("a", maxSignalPayloadLen+1024)
	body := fmt.Sprintf(`{"data":%q}`, huge)
	resp, err := http.Post(
		fmt.Sprintf("%s/api/familychat/conversations/%d/calls/big/offer", srv.URL, convID),
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("oversized payload: got %d, want 400", resp.StatusCode)
	}
	// No row should have been written.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM family_chat_calls`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("rejected offer persisted (%d rows)", n)
	}
}

// postJSON is a small POST + JSON helper. It fails the test on any non-2xx
// response so individual round-trip steps don't need their own status checks.
func postJSON(t *testing.T, url, body string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("POST %s: status %d", url, resp.StatusCode)
	}
}

// expectCallEvent waits for the next SSE event on sub and asserts the type
// and embedded call_id match. Each call relay event has the same payload
// shape, so a single helper covers all four event types.
func expectCallEvent(t *testing.T, sub *Subscriber, wantType, wantCallID string) {
	t.Helper()
	select {
	case evt, ok := <-sub.Events():
		if !ok {
			t.Fatal("subscriber channel closed")
		}
		if evt.Type != wantType {
			t.Fatalf("event type=%q, want %q", evt.Type, wantType)
		}
		data, ok := evt.Data.(map[string]any)
		if !ok {
			t.Fatalf("event data not map: %T", evt.Data)
		}
		if data["call_id"] != wantCallID {
			t.Errorf("call_id=%v, want %q", data["call_id"], wantCallID)
		}
	case <-time.After(time.Second):
		t.Fatalf("no %s event received in time", wantType)
	}
}

// TestCallSSE_RoundTripOverWire exercises the full subscribe→publish→decode
// loop over an HTTP connection rather than poking the hub directly. This is
// the integration counterpart to the per-handler unit tests above.
func TestCallSSE_RoundTripOverWire(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	hub := NewHub()
	mem := newMembershipSet([2]int64{1, convID}, [2]int64{2, convID})
	sender := func(int64, []byte) error { return nil }

	// One router that serves SSE for Bob and accepts relay POSTs from Alice.
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := auth.ContextWithUser(req.Context(), &auth.User{ID: 2, Name: "Bob"})
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		r.Get("/api/familychat/conversations/{id}/stream", StreamHandler(hub, mem.fn()))
	})
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := auth.ContextWithUser(req.Context(), &auth.User{ID: 1, Name: "Alice"})
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		r.Post("/api/familychat/conversations/{id}/calls/{call_id}/offer", callOfferHandler(db, hub, sender, true))
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	streamURL := fmt.Sprintf("%s/api/familychat/conversations/%d/stream", srv.URL, convID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status=%d", resp.StatusCode)
	}

	// Wait for the subscriber registration before publishing.
	deadline := time.Now().Add(2 * time.Second)
	for hub.subscriberCount(convID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber never registered")
		}
		time.Sleep(10 * time.Millisecond)
	}

	postJSON(t, fmt.Sprintf("%s/api/familychat/conversations/%d/calls/wire-call/offer", srv.URL, convID), `{"data":{"sdp":"v=0..."}}`)

	type result struct {
		event string
		data  string
	}
	resCh := make(chan result, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		var event, data string
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			case line == "":
				if event != "" {
					resCh <- result{event: event, data: data}
					return
				}
			}
		}
	}()

	select {
	case got := <-resCh:
		if got.event != EventCallOffer {
			t.Errorf("event=%q, want %q", got.event, EventCallOffer)
		}
		if !strings.Contains(got.data, `"call_id":"wire-call"`) {
			t.Errorf("data missing call_id: %s", got.data)
		}
		if !strings.Contains(got.data, `"sdp":"v=0..."`) {
			t.Errorf("data missing SDP payload: %s", got.data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no SSE event received in time")
	}
}
