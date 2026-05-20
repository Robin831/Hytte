package familychat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// withChiReactionParams installs both {id} and {messageID} on the request
// context so reaction handlers can resolve them.
func withChiReactionParams(r *http.Request, convID, msgID int64) *http.Request {
	return withChiParams(r, map[string]string{
		"id":        strconv.FormatInt(convID, 10),
		"messageID": strconv.FormatInt(msgID, 10),
	})
}

func TestValidateEmoji(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"single emoji", "👍", false},
		{"heart", "❤", false},
		{"heart with VS16", "❤️", false},
		{"tada", "🎉", false},
		{"skin tone", "👍🏽", false},
		{"zwj sequence (family)", "👨‍👩‍👧", false},
		{"flag", "🇳🇴", false},
		{"shortcode allowed", ":thumbsup:", false},
		{"shortcode unknown", ":sparkles:", true},
		{"empty", "", true},
		{"plain text", "hi", true},
		{"letters that look like emoji name", "thumbsup", true},
		{"too long", strings.Repeat("👍", 32), true},
		{"injection attempt", "<script>", true},
		{"shortcode with bad chars", ":bad chars:", true},
		{"shortcode missing trailing", ":thumbsup", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateEmoji(c.input)
			if (err != nil) != c.wantErr {
				t.Fatalf("validateEmoji(%q) err=%v, wantErr=%v", c.input, err, c.wantErr)
			}
		})
	}
}

func TestAddReactionHandler_Idempotent(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)
	msg, err := CreateMessage(db, convID, 1, "hi", "", "")
	if err != nil {
		t.Fatalf("create message: %v", err)
	}

	hub := NewHub()
	handler := addReactionHandler(db, hub)

	doAdd := func() int {
		t.Helper()
		body := `{"emoji":"👍"}`
		req := withUser(httptest.NewRequest("POST",
			fmt.Sprintf("/api/familychat/conversations/%d/messages/%d/reactions", convID, msg.ID),
			strings.NewReader(body)), 2)
		req.Header.Set("Content-Type", "application/json")
		req = withChiReactionParams(req, convID, msg.ID)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := doAdd(); code != http.StatusNoContent {
		t.Fatalf("first add: expected 204, got %d", code)
	}
	if code := doAdd(); code != http.StatusNoContent {
		t.Fatalf("second add: expected 204, got %d", code)
	}

	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM family_chat_message_reactions WHERE message_id = ? AND emoji = ?`,
		msg.ID, "👍",
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 reaction row after duplicate add, got %d", count)
	}
}

func TestAddReactionHandler_NonMember(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com") // not a member
	convID := seedConversation(t, db, 1, "Family", 2)
	msg, err := CreateMessage(db, convID, 1, "hi", "", "")
	if err != nil {
		t.Fatalf("create message: %v", err)
	}

	body := `{"emoji":"👍"}`
	req := withUser(httptest.NewRequest("POST",
		fmt.Sprintf("/api/familychat/conversations/%d/messages/%d/reactions", convID, msg.ID),
		strings.NewReader(body)), 3)
	req.Header.Set("Content-Type", "application/json")
	req = withChiReactionParams(req, convID, msg.ID)
	rec := httptest.NewRecorder()
	AddReactionHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-member, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddReactionHandler_InvalidEmoji(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")
	msg, err := CreateMessage(db, convID, 1, "hi", "", "")
	if err != nil {
		t.Fatalf("create message: %v", err)
	}

	body := `{"emoji":"<script>"}`
	req := withUser(httptest.NewRequest("POST",
		fmt.Sprintf("/api/familychat/conversations/%d/messages/%d/reactions", convID, msg.ID),
		strings.NewReader(body)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiReactionParams(req, convID, msg.ID)
	rec := httptest.NewRecorder()
	AddReactionHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid emoji, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddReactionHandler_MessageInOtherConversation(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convA := seedConversation(t, db, 1, "A", 2)
	convB := seedConversation(t, db, 1, "B", 2)
	msgA, err := CreateMessage(db, convA, 1, "hi", "", "")
	if err != nil {
		t.Fatalf("create message: %v", err)
	}

	// Try to react via convB even though msg belongs to convA.
	body := `{"emoji":"👍"}`
	req := withUser(httptest.NewRequest("POST",
		fmt.Sprintf("/api/familychat/conversations/%d/messages/%d/reactions", convB, msgA.ID),
		strings.NewReader(body)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiReactionParams(req, convB, msgA.ID)
	rec := httptest.NewRecorder()
	AddReactionHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-conversation reaction, got %d", rec.Code)
	}
}

func TestRemoveReactionHandler(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)
	msg, err := CreateMessage(db, convID, 1, "hi", "", "")
	if err != nil {
		t.Fatalf("create message: %v", err)
	}
	if _, err := addReaction(db, convID, msg.ID, 2, "👍"); err != nil {
		t.Fatalf("seed reaction: %v", err)
	}

	req := withUser(httptest.NewRequest("DELETE",
		fmt.Sprintf("/api/familychat/conversations/%d/messages/%d/reactions?emoji=%s", convID, msg.ID, "%F0%9F%91%8D"),
		nil), 2)
	req = withChiReactionParams(req, convID, msg.ID)
	rec := httptest.NewRecorder()
	RemoveReactionHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM family_chat_message_reactions WHERE message_id = ?`, msg.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected reaction removed, still have %d", count)
	}
}

func TestListMessagesHandler_EmbedsReactions(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")
	convID := seedConversation(t, db, 1, "Family", 2, 3)
	msg, err := CreateMessage(db, convID, 1, "hi", "", "")
	if err != nil {
		t.Fatalf("create message: %v", err)
	}
	if _, err := addReaction(db, convID, msg.ID, 1, "👍"); err != nil {
		t.Fatalf("react 1: %v", err)
	}
	if _, err := addReaction(db, convID, msg.ID, 2, "👍"); err != nil {
		t.Fatalf("react 2: %v", err)
	}
	if _, err := addReaction(db, convID, msg.ID, 3, "🎉"); err != nil {
		t.Fatalf("react 3: %v", err)
	}

	// Bob (id=2) requests the list. me=true for 👍 (he reacted), false for 🎉.
	idStr := strconv.FormatInt(convID, 10)
	req := withUser(httptest.NewRequest("GET", "/api/familychat/conversations/"+idStr+"/messages", nil), 2)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	ListMessagesHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Messages []Message `json:"messages"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(body.Messages))
	}
	got := body.Messages[0].Reactions
	if got == nil {
		t.Fatal("reactions map is nil")
	}
	thumb := got["👍"]
	if thumb == nil {
		t.Fatal("missing 👍 bucket")
	}
	if thumb.Count != 2 {
		t.Errorf("👍 count = %d, want 2", thumb.Count)
	}
	if !thumb.Me {
		t.Errorf("👍 me should be true for Bob")
	}
	if len(thumb.Users) != 2 {
		t.Errorf("👍 users len = %d, want 2", len(thumb.Users))
	}
	tada := got["🎉"]
	if tada == nil || tada.Count != 1 || tada.Me {
		t.Errorf("🎉 bucket wrong: %+v", tada)
	}
}

func TestListMessagesHandler_ReactionsCappedAt20Users(t *testing.T) {
	db := setupTestDB(t)
	const userCount = 25
	for i := int64(1); i <= userCount; i++ {
		makeUser(t, db, i, fmt.Sprintf("u%d@example.com", i))
	}
	// owner=1, all others added as members.
	members := make([]int64, 0, userCount-1)
	for i := int64(2); i <= userCount; i++ {
		members = append(members, i)
	}
	convID := seedConversation(t, db, 1, "Big", members...)
	msg, err := CreateMessage(db, convID, 1, "react me", "", "")
	if err != nil {
		t.Fatalf("create message: %v", err)
	}
	for i := int64(1); i <= userCount; i++ {
		if _, err := addReaction(db, convID, msg.ID, i, "🔥"); err != nil {
			t.Fatalf("react %d: %v", i, err)
		}
	}

	idStr := strconv.FormatInt(convID, 10)
	req := withUser(httptest.NewRequest("GET", "/api/familychat/conversations/"+idStr+"/messages", nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	ListMessagesHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Messages []Message `json:"messages"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	fire := body.Messages[0].Reactions["🔥"]
	if fire == nil {
		t.Fatal("missing 🔥 bucket")
	}
	if fire.Count != userCount {
		t.Errorf("count = %d, want %d", fire.Count, userCount)
	}
	if len(fire.Users) != maxReactionUsers {
		t.Errorf("users len = %d, want %d", len(fire.Users), maxReactionUsers)
	}
	wantExtra := userCount - maxReactionUsers
	if fire.ExtraCount != wantExtra {
		t.Errorf("extra = %d, want %d", fire.ExtraCount, wantExtra)
	}
	if !fire.Me {
		t.Errorf("user 1 reacted, expected me=true (even though they're past the cap)")
	}
}

func TestReactionHandler_PublishesSSEEvent(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)
	msg, err := CreateMessage(db, convID, 1, "hi", "", "")
	if err != nil {
		t.Fatalf("create message: %v", err)
	}

	hub := NewHub()
	bobSub := hub.Subscribe(2, convID)
	defer hub.Unsubscribe(convID, bobSub)

	// Alice (user 1) adds a reaction via the handler; Bob's SSE subscription
	// must receive a reaction_added event.
	handler := addReactionHandler(db, hub)
	body := `{"emoji":"👍"}`
	req := withUser(httptest.NewRequest("POST",
		fmt.Sprintf("/api/familychat/conversations/%d/messages/%d/reactions", convID, msg.ID),
		strings.NewReader(body)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiReactionParams(req, convID, msg.ID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("add reaction: %d %s", rec.Code, rec.Body.String())
	}

	select {
	case evt := <-bobSub.Events():
		if evt.Type != EventReactionAdded {
			t.Fatalf("event type = %q, want %q", evt.Type, EventReactionAdded)
		}
		payload, ok := evt.Data.(reactionEventPayload)
		if !ok {
			t.Fatalf("payload type = %T", evt.Data)
		}
		if payload.Emoji != "👍" || payload.MessageID != msg.ID || payload.UserID != 1 {
			t.Errorf("payload mismatch: %+v", payload)
		}
		if payload.Count != 1 {
			t.Errorf("count = %d, want 1", payload.Count)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive SSE event")
	}
}

func TestReactionHandler_PublishesSSEEventOverHTTP(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)
	msg, err := CreateMessage(db, convID, 1, "hi", "", "")
	if err != nil {
		t.Fatalf("create message: %v", err)
	}

	hub := NewHub()
	mem := newMembershipSet([2]int64{1, convID}, [2]int64{2, convID})

	r := chi.NewRouter()
	r.Get("/api/familychat/conversations/{id}/stream",
		withCtxUser(2, StreamHandler(hub, mem.fn())))
	r.Post("/api/familychat/conversations/{id}/messages/{messageID}/reactions",
		withCtxUser(1, addReactionHandler(db, hub)))

	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/api/familychat/conversations/%d/stream", srv.URL, convID), nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	deadline := time.Now().Add(2 * time.Second)
	for hub.subscriberCount(convID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber never registered")
		}
		time.Sleep(10 * time.Millisecond)
	}

	postResp, err := http.Post(
		fmt.Sprintf("%s/api/familychat/conversations/%d/messages/%d/reactions", srv.URL, convID, msg.ID),
		"application/json", strings.NewReader(`{"emoji":"👍"}`),
	)
	if err != nil {
		t.Fatalf("POST reaction: %v", err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", postResp.StatusCode)
	}

	// Read SSE frames until we get the event.
	type result struct {
		event string
		data  string
		err   error
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
		resCh <- result{err: scanner.Err()}
	}()

	select {
	case r := <-resCh:
		if r.err != nil {
			t.Fatalf("read body: %v", r.err)
		}
		if r.event != EventReactionAdded {
			t.Fatalf("event = %q, want %q", r.event, EventReactionAdded)
		}
		if !strings.Contains(r.data, `"emoji":"👍"`) {
			t.Fatalf("data missing emoji: %q", r.data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive SSE event in time")
	}
}

// withCtxUser wraps next so requests are served as if `userID` is logged in.
func withCtxUser(userID int64, next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, withUser(r, userID))
	}
}
