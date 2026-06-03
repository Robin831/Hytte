package familychat

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// sseFrame is one parsed SSE event: the optional id: line plus event/data.
type sseFrame struct {
	id    string
	event string
	data  string
}

// sseFrameChannel parses complete SSE frames off r and delivers them in order.
// The channel closes when the stream ends. Heartbeat comment lines (": ...")
// are skipped. id/event/data are trimmed of the single optional leading space.
func sseFrameChannel(r io.Reader) <-chan sseFrame {
	ch := make(chan sseFrame)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(r)
		var cur sseFrame
		var have bool
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, ":"):
				// heartbeat / comment — ignore
			case strings.HasPrefix(line, "id:"):
				cur.id = strings.TrimSpace(line[len("id:"):])
				have = true
			case strings.HasPrefix(line, "event:"):
				cur.event = strings.TrimSpace(line[len("event:"):])
				have = true
			case strings.HasPrefix(line, "data:"):
				cur.data = strings.TrimSpace(line[len("data:"):])
				have = true
			case line == "":
				if have && cur.event != "" {
					ch <- cur
				}
				cur = sseFrame{}
				have = false
			}
		}
	}()
	return ch
}

// nextFrame waits up to d for the next SSE frame, failing the test on timeout
// or premature close.
func nextFrame(t *testing.T, ch <-chan sseFrame, d time.Duration) sseFrame {
	t.Helper()
	select {
	case f, ok := <-ch:
		if !ok {
			t.Fatal("SSE stream closed before a frame arrived")
		}
		return f
	case <-time.After(d):
		t.Fatal("timed out waiting for SSE frame")
	}
	return sseFrame{}
}

func TestStreamHandler_ReplaysBacklogBeforeLiveEvents(t *testing.T) {
	hub := NewHub()
	const convID int64 = 400
	mem := newMembershipSet([2]int64{1, convID})
	user := &auth.User{ID: 1, Email: "a@example.com", Name: "A"}

	edited := "2026-01-01T00:00:00.000Z"
	backfill := BackfillFn(func(cid, uid, since int64) ([]Message, error) {
		if cid != convID || uid != 1 || since != 3 {
			t.Errorf("backfill args: conv=%d user=%d since=%d", cid, uid, since)
		}
		return []Message{
			{ID: 5, ConversationID: convID, SenderUserID: 2, Body: "after gap", CreatedAt: edited, Reactions: map[string]*ReactionSummary{}},
			{ID: 2, ConversationID: convID, SenderUserID: 1, Body: "edited body", CreatedAt: edited, EditedAt: &edited, Reactions: map[string]*ReactionSummary{}},
		}, nil
	})

	srv := httptest.NewServer(newBackfillRouter(t, hub, mem.fn(), backfill, user))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/familychat/conversations/400/stream?since_message_id=3", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	frames := sseFrameChannel(resp.Body)

	// Backlog first: a never-seen message (id 5) becomes message_new; an edit of
	// an older message (id 2) becomes message_edited. Both carry id: lines.
	f1 := nextFrame(t, frames, 3*time.Second)
	if f1.event != EventMessageNew || f1.id != "5" {
		t.Fatalf("frame 1 = {event:%q id:%q}, want message_new/5", f1.event, f1.id)
	}
	if !strings.Contains(f1.data, `"after gap"`) {
		t.Fatalf("frame 1 data missing body: %q", f1.data)
	}
	f2 := nextFrame(t, frames, 3*time.Second)
	if f2.event != EventMessageEdited || f2.id != "2" {
		t.Fatalf("frame 2 = {event:%q id:%q}, want message_edited/2", f2.event, f2.id)
	}

	// Now a live event arrives after the backlog has been flushed.
	deadline := time.Now().Add(2 * time.Second)
	for hub.subscriberCount(convID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	hub.Publish(convID, Event{Type: EventMessageNew, ID: 6, Data: map[string]any{"message": map[string]any{"id": 6, "body": "live"}}})
	f3 := nextFrame(t, frames, 3*time.Second)
	if f3.event != EventMessageNew || f3.id != "6" {
		t.Fatalf("frame 3 = {event:%q id:%q}, want message_new/6", f3.event, f3.id)
	}
}

func TestStreamHandler_NoResumeIDSkipsBackfill(t *testing.T) {
	hub := NewHub()
	const convID int64 = 410
	mem := newMembershipSet([2]int64{1, convID})
	user := &auth.User{ID: 1, Email: "a@example.com", Name: "A"}

	var calls int32
	backfill := BackfillFn(func(cid, uid, since int64) ([]Message, error) {
		atomic.AddInt32(&calls, 1)
		return nil, nil
	})

	srv := httptest.NewServer(newBackfillRouter(t, hub, mem.fn(), backfill, user))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// No since_message_id and no Last-Event-ID header → behave as a live feed.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/familychat/conversations/410/stream", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (membership verified), got %d", resp.StatusCode)
	}

	frames := sseFrameChannel(resp.Body)
	deadline := time.Now().Add(2 * time.Second)
	for hub.subscriberCount(convID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	hub.Publish(convID, Event{Type: EventMessageNew, ID: 9, Data: map[string]any{"message": map[string]any{"id": 9}}})
	f := nextFrame(t, frames, 3*time.Second)
	if f.event != EventMessageNew || f.id != "9" {
		t.Fatalf("live frame = {event:%q id:%q}, want message_new/9", f.event, f.id)
	}
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Fatalf("backfill should not run without a resume id, called %d times", n)
	}
}

func TestStreamHandler_InvalidResumeIDSkipsBackfill(t *testing.T) {
	hub := NewHub()
	const convID int64 = 420
	mem := newMembershipSet([2]int64{1, convID})
	user := &auth.User{ID: 1, Email: "a@example.com", Name: "A"}

	var calls int32
	backfill := BackfillFn(func(cid, uid, since int64) ([]Message, error) {
		atomic.AddInt32(&calls, 1)
		return nil, nil
	})

	srv := httptest.NewServer(newBackfillRouter(t, hub, mem.fn(), backfill, user))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/familychat/conversations/420/stream?since_message_id=not-a-number", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for invalid resume id, got %d", resp.StatusCode)
	}
	deadline := time.Now().Add(2 * time.Second)
	for hub.subscriberCount(convID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Fatalf("backfill should not run for an unparseable resume id, called %d times", n)
	}
}

func TestStreamHandler_EphemeralEventHasNoIDLine(t *testing.T) {
	hub := NewHub()
	const convID int64 = 430
	mem := newMembershipSet([2]int64{1, convID})
	user := &auth.User{ID: 1, Email: "a@example.com", Name: "A"}

	srv := httptest.NewServer(newBackfillRouter(t, hub, mem.fn(), nil, user))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/familychat/conversations/430/stream", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	frames := sseFrameChannel(resp.Body)
	deadline := time.Now().Add(2 * time.Second)
	for hub.subscriberCount(convID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Typing is ephemeral: ID left zero → no id: line, so it never becomes a
	// resume point for a reconnecting browser.
	hub.Publish(convID, Event{Type: EventTyping, Data: map[string]any{"conversation_id": convID, "user_id": int64(2)}})
	f := nextFrame(t, frames, 3*time.Second)
	if f.event != EventTyping {
		t.Fatalf("expected typing event, got %q", f.event)
	}
	if f.id != "" {
		t.Fatalf("ephemeral event must not carry an id: line, got id=%q", f.id)
	}
}

// fakeMembership returns a MembershipFn that consults a closed-over set.
type membershipSet struct {
	mu      sync.Mutex
	allowed map[[2]int64]bool
}

func newMembershipSet(pairs ...[2]int64) *membershipSet {
	m := &membershipSet{allowed: make(map[[2]int64]bool)}
	for _, p := range pairs {
		m.allowed[p] = true
	}
	return m
}

func (m *membershipSet) fn() MembershipFn {
	return func(userID, convID int64) (bool, error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.allowed[[2]int64{userID, convID}], nil
	}
}

// newAuthRouter wires the SSE handler at the real route path with a fake
// user injected into the request context. Membership is checked via the
// provided function.
func newAuthRouter(t *testing.T, hub *Hub, membership MembershipFn, user *auth.User) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.ContextWithUser(req.Context(), user)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Get("/api/familychat/conversations/{id}/stream", StreamHandler(hub, membership, nil))
	return r
}

// newBackfillRouter is like newAuthRouter but wires a backfill function so the
// resume/replay path can be exercised end-to-end over the wire.
func newBackfillRouter(t *testing.T, hub *Hub, membership MembershipFn, backfill BackfillFn, user *auth.User) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.ContextWithUser(req.Context(), user)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Get("/api/familychat/conversations/{id}/stream", StreamHandler(hub, membership, backfill))
	return r
}

func TestStreamHandler_NonMemberReturns404(t *testing.T) {
	hub := NewHub()
	// User 1 belongs to conv 100 only; request for conv 999 should 404.
	mem := newMembershipSet([2]int64{1, 100})
	user := &auth.User{ID: 1, Email: "a@example.com", Name: "A"}

	srv := httptest.NewServer(newAuthRouter(t, hub, mem.fn(), user))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/familychat/conversations/999/stream")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for non-member, got %d", resp.StatusCode)
	}
}

func TestStreamHandler_ReceivesMessageNew(t *testing.T) {
	hub := NewHub()
	const convID int64 = 100
	mem := newMembershipSet([2]int64{1, convID})
	user := &auth.User{ID: 1, Email: "a@example.com", Name: "A"}

	srv := httptest.NewServer(newAuthRouter(t, hub, mem.fn(), user))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/familychat/conversations/100/stream", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("expected SSE content type, got %q", ct)
	}

	// Wait until the handler has subscribed before publishing, otherwise the
	// publish races the subscribe and the event is dropped.
	deadline := time.Now().Add(2 * time.Second)
	for hub.subscriberCount(convID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber never registered with hub")
		}
		time.Sleep(10 * time.Millisecond)
	}

	hub.Publish(convID, Event{
		Type: EventMessageNew,
		Data: map[string]any{
			"message": map[string]any{"id": 7, "body": "hello"},
		},
	})

	// Read SSE lines until we see the event/data pair.
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
			t.Fatalf("scanner: %v", r.err)
		}
		if r.event != EventMessageNew {
			t.Fatalf("expected event %q, got %q", EventMessageNew, r.event)
		}
		if !strings.Contains(r.data, `"body":"hello"`) {
			t.Fatalf("expected data to contain message body, got %q", r.data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive SSE event in time")
	}
}

func TestStreamHandler_UnsubscribesOnClientDisconnect(t *testing.T) {
	hub := NewHub()
	const convID int64 = 200
	mem := newMembershipSet([2]int64{1, convID})
	user := &auth.User{ID: 1, Email: "a@example.com", Name: "A"}

	srv := httptest.NewServer(newAuthRouter(t, hub, mem.fn(), user))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/familychat/conversations/200/stream", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	// Wait for the subscriber to register.
	deadline := time.Now().Add(2 * time.Second)
	for hub.subscriberCount(convID) == 0 {
		if time.Now().After(deadline) {
			resp.Body.Close()
			cancel()
			t.Fatal("subscriber never registered with hub")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Cancel the client request — the handler should unsubscribe.
	cancel()
	_ = resp.Body.Close()

	deadline = time.Now().Add(2 * time.Second)
	for hub.subscriberCount(convID) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("expected subscriber removed after disconnect, still have %d", hub.subscriberCount(convID))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := hub.conversationCount(); got != 0 {
		t.Fatalf("expected hub to reclaim conversation key, got %d", got)
	}
}

// TestStreamHandler_ReceivesEventViaPublishRoute verifies the SSE path
// end-to-end via an HTTP request rather than a direct hub.Publish call.
// A stub POST endpoint mimics what the future POST /messages handler will do:
// persist a row then call hub.Publish. This exercises the full
// subscribe→receive→decode cycle over the wire.
func TestStreamHandler_ReceivesEventViaPublishRoute(t *testing.T) {
	hub := NewHub()
	const convID int64 = 300
	mem := newMembershipSet([2]int64{1, convID})
	user := &auth.User{ID: 1, Email: "a@example.com", Name: "A"}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.ContextWithUser(req.Context(), user)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Get("/api/familychat/conversations/{id}/stream", StreamHandler(hub, mem.fn(), nil))
	// Stub that publishes an event, standing in for a real POST /messages handler.
	r.Post("/api/familychat/conversations/{id}/publish", func(w http.ResponseWriter, req *http.Request) {
		hub.Publish(convID, Event{Type: EventMessageNew, Data: map[string]any{"body": "via-post"}})
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/familychat/conversations/300/stream", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	deadline := time.Now().Add(2 * time.Second)
	for hub.subscriberCount(convID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber never registered")
		}
		time.Sleep(10 * time.Millisecond)
	}

	postResp, err := http.Post(srv.URL+"/api/familychat/conversations/300/publish", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	postResp.Body.Close()

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
			t.Fatalf("scanner: %v", r.err)
		}
		if r.event != EventMessageNew {
			t.Fatalf("expected %q, got %q", EventMessageNew, r.event)
		}
		if !strings.Contains(r.data, `"body":"via-post"`) {
			t.Fatalf("expected body in data, got %q", r.data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive SSE event in time")
	}
}

func TestStreamHandler_InvalidConversationID(t *testing.T) {
	hub := NewHub()
	mem := newMembershipSet()
	user := &auth.User{ID: 1, Email: "a@example.com", Name: "A"}

	srv := httptest.NewServer(newAuthRouter(t, hub, mem.fn(), user))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/familychat/conversations/not-a-number/stream")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid id, got %d", resp.StatusCode)
	}
}

func TestStreamHandler_MembershipCheckError(t *testing.T) {
	hub := NewHub()
	user := &auth.User{ID: 1, Email: "a@example.com", Name: "A"}
	errFn := MembershipFn(func(userID, convID int64) (bool, error) {
		return false, errors.New("db unavailable")
	})

	srv := httptest.NewServer(newAuthRouter(t, hub, errFn, user))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/familychat/conversations/100/stream")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 for membership check error, got %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body["error"] != "internal error" {
		t.Fatalf("expected error=%q, got %q", "internal error", body["error"])
	}
}
