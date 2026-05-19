package familychat

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

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
	r.Get("/api/familychat/conversations/{id}/stream", StreamHandler(hub, membership))
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
	r.Get("/api/familychat/conversations/{id}/stream", StreamHandler(hub, mem.fn()))
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
