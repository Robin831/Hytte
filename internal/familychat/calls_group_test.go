package familychat

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// groupCallsRouter wires the group-call lifecycle endpoints (join/leave) plus
// the reused offer relay behind a request-context user injector.
func groupCallsRouter(t *testing.T, db *sql.DB, hub *Hub, sender PushSenderFunc, user *auth.User) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.ContextWithUser(req.Context(), user)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Post("/api/familychat/conversations/{id}/calls/{call_id}/join", callJoinHandler(db, hub, sender, true))
	r.Post("/api/familychat/conversations/{id}/calls/{call_id}/leave", callLeaveHandler(db, hub))
	r.Post("/api/familychat/conversations/{id}/calls/{call_id}/offer", callOfferHandler(db, hub, sender, true))
	return r
}

func TestJoinCall_ReturnsExistingPeersAndAnswers(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")
	convID := seedConversation(t, db, 1, "Family", 2, 3)

	callID := "group-1"

	// First joiner sees no peers and the call sits in 'ringing'.
	peers, err := JoinCall(db, convID, 1, callID, CallKindVideo)
	if err != nil {
		t.Fatalf("join alice: %v", err)
	}
	if len(peers) != 0 {
		t.Errorf("first joiner peers=%v, want empty", peers)
	}
	if got, _ := GetCallStatus(db, convID, callID); got != CallStatusRinging {
		t.Errorf("after first join status=%q, want ringing", got)
	}
	if got, _ := GetCallKind(db, convID, callID); got != CallKindVideo {
		t.Errorf("kind=%q, want video", got)
	}

	// Second joiner sees alice and the call flips to 'answered'.
	peers, err = JoinCall(db, convID, 2, callID, CallKindVideo)
	if err != nil {
		t.Fatalf("join bob: %v", err)
	}
	if len(peers) != 1 || peers[0] != 1 {
		t.Errorf("bob peers=%v, want [1]", peers)
	}
	if got, _ := GetCallStatus(db, convID, callID); got != CallStatusAnswered {
		t.Errorf("after second join status=%q, want answered", got)
	}

	// Third joiner sees both earlier participants in ascending order.
	peers, err = JoinCall(db, convID, 3, callID, CallKindVideo)
	if err != nil {
		t.Fatalf("join carol: %v", err)
	}
	if len(peers) != 2 || peers[0] != 1 || peers[1] != 2 {
		t.Errorf("carol peers=%v, want [1 2]", peers)
	}

	n, err := CountActiveParticipants(db, convID, callID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Errorf("active=%d, want 3", n)
	}
}

func TestLeaveCall_EndsWhenRoomEmpties(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	callID := "group-leave"
	if _, err := JoinCall(db, convID, 1, callID, CallKindVoice); err != nil {
		t.Fatalf("join alice: %v", err)
	}
	if _, err := JoinCall(db, convID, 2, callID, CallKindVoice); err != nil {
		t.Fatalf("join bob: %v", err)
	}

	remaining, status, err := LeaveCall(db, convID, 1, callID)
	if err != nil {
		t.Fatalf("leave alice: %v", err)
	}
	if remaining != 1 {
		t.Errorf("remaining=%d, want 1", remaining)
	}
	if status != "" {
		t.Errorf("status=%q, want empty (room not empty)", status)
	}

	remaining, status, err = LeaveCall(db, convID, 2, callID)
	if err != nil {
		t.Fatalf("leave bob: %v", err)
	}
	if remaining != 0 {
		t.Errorf("remaining=%d, want 0", remaining)
	}
	// Call was answered (two participants), so it ends rather than being missed.
	if status != CallStatusEnded {
		t.Errorf("final status=%q, want ended", status)
	}
	if got, _ := GetCallStatus(db, convID, callID); got != CallStatusEnded {
		t.Errorf("envelope status=%q, want ended", got)
	}
}

func TestLeaveCall_MissedWhenNeverAnswered(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")

	callID := "group-missed"
	if _, err := JoinCall(db, convID, 1, callID, CallKindVoice); err != nil {
		t.Fatalf("join: %v", err)
	}
	// Lone participant leaves before anyone else joined → missed.
	_, status, err := LeaveCall(db, convID, 1, callID)
	if err != nil {
		t.Fatalf("leave: %v", err)
	}
	if status != CallStatusMissed {
		t.Errorf("status=%q, want missed", status)
	}
}

func TestCallJoinHandler_ReturnsParticipantsAndPublishes(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")
	convID := seedConversation(t, db, 1, "Family", 2, 3)

	// Seed alice as an existing participant directly so bob's join over HTTP
	// finds a peer.
	if _, err := JoinCall(db, convID, 1, "g9", CallKindVideo); err != nil {
		t.Fatalf("seed join: %v", err)
	}

	hub := NewHub()
	carolSub := hub.Subscribe(3, convID)
	defer hub.Unsubscribe(convID, carolSub)

	sender := func(int64, []byte) error { return nil }
	srv := httptest.NewServer(groupCallsRouter(t, db, hub, sender, &auth.User{ID: 2, Name: "Bob"}))
	defer srv.Close()

	body := postJSONResp(t,
		fmt.Sprintf("%s/api/familychat/conversations/%d/calls/g9/join", srv.URL, convID),
		`{"kind":"video"}`,
	)
	var resp struct {
		CallID       string  `json:"call_id"`
		Kind         string  `json:"kind"`
		Participants []int64 `json:"participants"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode join resp: %v", err)
	}
	if resp.Kind != CallKindVideo {
		t.Errorf("kind=%q, want video", resp.Kind)
	}
	if len(resp.Participants) != 1 || resp.Participants[0] != 1 {
		t.Errorf("participants=%v, want [1]", resp.Participants)
	}

	// Carol (live on SSE) should see bob's call_join event.
	expectCallEvent(t, carolSub, EventCallJoin, "g9")
}

func TestCallJoinHandler_FirstJoinRingsOfflineMembers(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")
	convID := seedConversation(t, db, 1, "Family", 2, 3)

	hub := NewHub()
	// Bob is live on SSE — push must skip him; carol is offline and gets rung.
	bobSub := hub.Subscribe(2, convID)
	defer hub.Unsubscribe(convID, bobSub)
	go func() {
		for range bobSub.Events() {
		}
	}()

	var mu sync.Mutex
	var rung []int64
	sender := func(uid int64, _ []byte) error {
		mu.Lock()
		defer mu.Unlock()
		rung = append(rung, uid)
		return nil
	}

	srv := httptest.NewServer(groupCallsRouter(t, db, hub, sender, &auth.User{ID: 1, Name: "Alice"}))
	defer srv.Close()

	postJSONResp(t, fmt.Sprintf("%s/api/familychat/conversations/%d/calls/ring/join", srv.URL, convID), `{}`)

	mu.Lock()
	defer mu.Unlock()
	if len(rung) != 1 || rung[0] != 3 {
		t.Errorf("rung=%v, want [3] (offline member only)", rung)
	}
}

func TestCallLeaveHandler_PublishesLeaveAndEnd(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	// Two participants in the room.
	if _, err := JoinCall(db, convID, 1, "lv", CallKindVoice); err != nil {
		t.Fatalf("join alice: %v", err)
	}
	if _, err := JoinCall(db, convID, 2, "lv", CallKindVoice); err != nil {
		t.Fatalf("join bob: %v", err)
	}

	hub := NewHub()
	watcher := hub.Subscribe(2, convID)
	defer hub.Unsubscribe(convID, watcher)

	sender := func(int64, []byte) error { return nil }
	srv := httptest.NewServer(groupCallsRouter(t, db, hub, sender, &auth.User{ID: 1, Name: "Alice"}))
	defer srv.Close()

	// Alice leaves — only a call_leave, room still has bob.
	postNoContent(t, fmt.Sprintf("%s/api/familychat/conversations/%d/calls/lv/leave", srv.URL, convID))
	expectCallEvent(t, watcher, EventCallLeave, "lv")
	select {
	case evt := <-watcher.Events():
		t.Fatalf("unexpected extra event after non-final leave: %+v", evt)
	case <-time.After(100 * time.Millisecond):
	}

	// Bob leaves — last participant, so call_leave then a terminal call_end.
	srv2 := httptest.NewServer(groupCallsRouter(t, db, hub, sender, &auth.User{ID: 2, Name: "Bob"}))
	defer srv2.Close()
	postNoContent(t, fmt.Sprintf("%s/api/familychat/conversations/%d/calls/lv/leave", srv2.URL, convID))
	expectCallEvent(t, watcher, EventCallLeave, "lv")
	expectCallEvent(t, watcher, EventCallEnd, "lv")
}

func TestCallOffer_TargetedIsRelayOnly(t *testing.T) {
	// A targeted (group mesh) offer must not create a call envelope or send a
	// webpush — those are owned by the join endpoint — but it must relay the
	// SDP with the to_user_id so only the addressed peer reacts.
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")
	convID := seedConversation(t, db, 1, "Family", 2, 3)

	hub := NewHub()
	bobSub := hub.Subscribe(2, convID)
	defer hub.Unsubscribe(convID, bobSub)

	var pushed int
	sender := func(int64, []byte) error { pushed++; return nil }
	srv := httptest.NewServer(groupCallsRouter(t, db, hub, sender, &auth.User{ID: 1, Name: "Alice"}))
	defer srv.Close()

	postJSONResp(t,
		fmt.Sprintf("%s/api/familychat/conversations/%d/calls/mesh/offer", srv.URL, convID),
		`{"data":{"type":"offer","sdp":"v=0..."},"kind":"video","to_user_id":2}`,
	)

	// No call envelope persisted by the targeted relay.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM family_chat_calls WHERE call_id = ?`, "mesh").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("targeted offer wrote %d call row(s), want 0", n)
	}
	if pushed != 0 {
		t.Errorf("targeted offer triggered %d push(es), want 0", pushed)
	}

	select {
	case evt, ok := <-bobSub.Events():
		if !ok {
			t.Fatal("subscriber channel closed")
		}
		data, ok := evt.Data.(map[string]any)
		if !ok {
			t.Fatalf("event data not map: %T", evt.Data)
		}
		if data["to_user_id"] != int64(2) {
			t.Errorf("to_user_id=%v, want 2", data["to_user_id"])
		}
	case <-time.After(time.Second):
		t.Fatal("no targeted call_offer relayed")
	}
}

// postJSONResp POSTs a JSON body and returns the response body, failing on a
// non-2xx status.
func postJSONResp(t *testing.T, url, body string) []byte {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("POST %s: status %d", url, resp.StatusCode)
	}
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return out
}

// postNoContent POSTs an empty body and asserts a 204 response.
func postNoContent(t *testing.T, url string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST %s: status %d, want 204", url, resp.StatusCode)
	}
}
