package training

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// newStreamRouter wires the SSE handler at the real route path with a fake user
// injected into the request context.
func newStreamRouter(t *testing.T, hub *Hub, user *auth.User) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.ContextWithUser(req.Context(), user)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Get("/api/training/events", StreamHandler(hub))
	return r
}

func TestStreamHandler_Unauthenticated(t *testing.T) {
	hub := NewHub()
	// Router that injects no user into the context.
	r := chi.NewRouter()
	r.Get("/api/training/events", StreamHandler(hub))

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/training/events")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated, got %d", resp.StatusCode)
	}
}

func TestStreamHandler_SetsSSEHeaders(t *testing.T) {
	hub := NewHub()
	user := &auth.User{ID: 1, Email: "a@example.com", Name: "A"}

	srv := httptest.NewServer(newStreamRouter(t, hub, user))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/training/events", nil)
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
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("expected Cache-Control no-cache, got %q", cc)
	}
}

func TestStreamHandler_ReceivesWorkoutNew(t *testing.T) {
	hub := NewHub()
	const userID int64 = 1
	user := &auth.User{ID: userID, Email: "a@example.com", Name: "A"}

	srv := httptest.NewServer(newStreamRouter(t, hub, user))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/training/events", nil)
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

	// Wait until the handler has subscribed before publishing, otherwise the
	// publish races the subscribe and the event is dropped.
	deadline := time.Now().Add(2 * time.Second)
	for hub.subscriberCount(userID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber never registered with hub")
		}
		time.Sleep(10 * time.Millisecond)
	}

	hub.Publish(userID, Event{Type: EventWorkoutNew, LatestID: 123})

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
		if r.event != EventWorkoutNew {
			t.Fatalf("expected event %q, got %q", EventWorkoutNew, r.event)
		}
		if !strings.Contains(r.data, `"latest_id":123`) {
			t.Fatalf("expected data to contain latest_id, got %q", r.data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive SSE event in time")
	}
}

func TestStreamHandler_NoCrossUserLeakage(t *testing.T) {
	hub := NewHub()
	const userID int64 = 1
	user := &auth.User{ID: userID, Email: "a@example.com", Name: "A"}

	srv := httptest.NewServer(newStreamRouter(t, hub, user))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/training/events", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	deadline := time.Now().Add(2 * time.Second)
	for hub.subscriberCount(userID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber never registered with hub")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Publish for a different user — user 1's stream must not receive it.
	hub.Publish(2, Event{Type: EventWorkoutNew, LatestID: 999})

	gotCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				gotCh <- line
				return
			}
		}
	}()

	select {
	case line := <-gotCh:
		t.Fatalf("received event meant for another user: %q", line)
	case <-time.After(300 * time.Millisecond):
		// good — no cross-user event delivered
	}
}

func TestStreamHandler_UnsubscribesOnClientDisconnect(t *testing.T) {
	hub := NewHub()
	const userID int64 = 1
	user := &auth.User{ID: userID, Email: "a@example.com", Name: "A"}

	srv := httptest.NewServer(newStreamRouter(t, hub, user))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/training/events", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	// Wait for the subscriber to register.
	deadline := time.Now().Add(2 * time.Second)
	for hub.subscriberCount(userID) == 0 {
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
	for hub.subscriberCount(userID) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("expected subscriber removed after disconnect, still have %d", hub.subscriberCount(userID))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := hub.userCount(); got != 0 {
		t.Fatalf("expected hub to reclaim user key, got %d", got)
	}
}
