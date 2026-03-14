package webhooks

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// sqliteTimestampFormat is the format used by SQLite's CURRENT_TIMESTAMP.
const sqliteTimestampFormat = "2006-01-02 15:04:05"

// toRFC3339 converts a SQLite timestamp string to RFC3339 format.
// Returns the original string if parsing fails.
func toRFC3339(s string) string {
	t, err := time.Parse(sqliteTimestampFormat, s)
	if err != nil {
		return s
	}
	return t.UTC().Format(time.RFC3339)
}

// Endpoint represents a webhook endpoint owned by a user.
type Endpoint struct {
	ID        string `json:"id"`
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

// Request represents a captured incoming webhook request.
type Request struct {
	ID         int64             `json:"id"`
	EndpointID string            `json:"endpoint_id"`
	Method     string            `json:"method"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	Query      string            `json:"query"`
	RemoteAddr string            `json:"remote_addr"`
	ReceivedAt string            `json:"received_at"`
}

// Hub manages SSE subscribers for live-updating webhook requests.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan *Request]struct{} // endpoint_id -> set of channels
}

// NewHub creates a new SSE hub.
func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[string]map[chan *Request]struct{}),
	}
}

func (h *Hub) subscribe(endpointID string) chan *Request {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan *Request, 16)
	if h.subscribers[endpointID] == nil {
		h.subscribers[endpointID] = make(map[chan *Request]struct{})
	}
	h.subscribers[endpointID][ch] = struct{}{}
	return ch
}

func (h *Hub) unsubscribe(endpointID string, ch chan *Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if subs, ok := h.subscribers[endpointID]; ok {
		delete(subs, ch)
		if len(subs) == 0 {
			delete(h.subscribers, endpointID)
		}
	}
	close(ch)
}

func (h *Hub) publish(endpointID string, req *Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers[endpointID] {
		select {
		case ch <- req:
		default:
			// Drop if subscriber is slow.
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func generateID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random ID: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ListEndpoints returns all webhook endpoints for the authenticated user.
func ListEndpoints(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		rows, err := db.Query(
			"SELECT id, user_id, name, created_at FROM webhook_endpoints WHERE user_id = ? ORDER BY created_at DESC",
			user.ID,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
			return
		}
		defer rows.Close()

		endpoints := []Endpoint{}
		for rows.Next() {
			var ep Endpoint
			if err := rows.Scan(&ep.ID, &ep.UserID, &ep.Name, &ep.CreatedAt); err != nil {
				continue
			}
			ep.CreatedAt = toRFC3339(ep.CreatedAt)
			endpoints = append(endpoints, ep)
		}

		writeJSON(w, http.StatusOK, map[string]any{"endpoints": endpoints})
	}
}

// CreateEndpoint creates a new webhook endpoint for the authenticated user.
func CreateEndpoint(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			if err == io.EOF {
				// Empty body: treat as no name provided, will use default below.
				body.Name = ""
			} else {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
				return
			}
		}

		id, err := generateID()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate ID"})
			return
		}
		if body.Name == "" {
			n := min(6, len(id))
			body.Name = "Webhook " + id[:n]
		}

		_, err = db.Exec(
			"INSERT INTO webhook_endpoints (id, user_id, name) VALUES (?, ?, ?)",
			id, user.ID, body.Name,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
			return
		}

		ep := Endpoint{ID: id, UserID: user.ID, Name: body.Name}
		// Fetch the created_at from DB and convert to RFC3339.
		if err := db.QueryRow("SELECT created_at FROM webhook_endpoints WHERE id = ?", id).Scan(&ep.CreatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
			return
		}
		ep.CreatedAt = toRFC3339(ep.CreatedAt)

		writeJSON(w, http.StatusCreated, ep)
	}
}

// DeleteEndpoint removes a webhook endpoint and all its captured requests.
func DeleteEndpoint(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		id := chi.URLParam(r, "endpointID")

		result, err := db.Exec(
			"DELETE FROM webhook_endpoints WHERE id = ? AND user_id = ?",
			id, user.ID,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
			return
		}

		affected, _ := result.RowsAffected()
		if affected == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// ListRequests returns captured webhook requests for an endpoint owned by the user.
func ListRequests(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		endpointID := chi.URLParam(r, "endpointID")

		// Verify ownership.
		var ownerID int64
		err := db.QueryRow("SELECT user_id FROM webhook_endpoints WHERE id = ?", endpointID).Scan(&ownerID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
			}
			return
		}
		if ownerID != user.ID {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
			return
		}

		rows, err := db.Query(
			"SELECT id, endpoint_id, method, headers, body, query, remote_addr, received_at FROM webhook_requests WHERE endpoint_id = ? ORDER BY received_at DESC, id DESC LIMIT 100",
			endpointID,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
			return
		}
		defer rows.Close()

		requests := []Request{}
		for rows.Next() {
			var req Request
			var headersJSON string
			if err := rows.Scan(&req.ID, &req.EndpointID, &req.Method, &headersJSON, &req.Body, &req.Query, &req.RemoteAddr, &req.ReceivedAt); err != nil {
				continue
			}
			json.Unmarshal([]byte(headersJSON), &req.Headers)
			if req.Headers == nil {
				req.Headers = map[string]string{}
			}
			req.ReceivedAt = toRFC3339(req.ReceivedAt)
			requests = append(requests, req)
		}

		writeJSON(w, http.StatusOK, map[string]any{"requests": requests})
	}
}

// ClearRequests deletes all captured requests for an endpoint.
func ClearRequests(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		endpointID := chi.URLParam(r, "endpointID")

		// Verify ownership.
		var ownerID int64
		err := db.QueryRow("SELECT user_id FROM webhook_endpoints WHERE id = ?", endpointID).Scan(&ownerID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
			}
			return
		}
		if ownerID != user.ID {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
			return
		}

		if _, err := db.Exec("DELETE FROM webhook_requests WHERE endpoint_id = ?", endpointID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
	}
}

// ReceiveWebhook captures any incoming HTTP request to a webhook endpoint.
// This handler is public — no authentication required.
func ReceiveWebhook(db *sql.DB, hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		endpointID := chi.URLParam(r, "endpointID")

		// Check endpoint exists.
		var exists int
		err := db.QueryRow("SELECT 1 FROM webhook_endpoints WHERE id = ?", endpointID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
			return
		}

		// Read body (limit to 1MB).
		bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
			return
		}

		// Collect headers.
		headers := make(map[string]string)
		keys := make([]string, 0, len(r.Header))
		for k := range r.Header {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			headers[k] = strings.Join(r.Header[k], ", ")
		}
		headersJSON, _ := json.Marshal(headers)

		query := r.URL.RawQuery
		remoteAddr := r.RemoteAddr

		result, err := db.Exec(
			"INSERT INTO webhook_requests (endpoint_id, method, headers, body, query, remote_addr) VALUES (?, ?, ?, ?, ?, ?)",
			endpointID, r.Method, string(headersJSON), string(bodyBytes), query, remoteAddr,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
			return
		}

		reqID, err := result.LastInsertId()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
			return
		}
		req := &Request{
			ID:         reqID,
			EndpointID: endpointID,
			Method:     r.Method,
			Headers:    headers,
			Body:       string(bodyBytes),
			Query:      query,
			RemoteAddr: remoteAddr,
		}
		// Fetch the received_at from DB and convert to RFC3339.
		if err := db.QueryRow("SELECT received_at FROM webhook_requests WHERE id = ?", reqID).Scan(&req.ReceivedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
			return
		}
		req.ReceivedAt = toRFC3339(req.ReceivedAt)

		// Retention: keep only the last 100 requests per endpoint to prevent unbounded growth.
		if _, err := db.Exec(
			"DELETE FROM webhook_requests WHERE endpoint_id = ? AND id NOT IN (SELECT id FROM webhook_requests WHERE endpoint_id = ? ORDER BY received_at DESC, id DESC LIMIT 100)",
			endpointID, endpointID,
		); err != nil {
			log.Printf("retention cleanup failed for endpoint %s: %v", endpointID, err)
		}

		// Notify SSE subscribers.
		hub.publish(endpointID, req)

		// Dispatch push notifications asynchronously — fire-and-forget.
		go dispatchPushNotifications(
			context.Background(), db, nil, endpointID, reqID,
			r.Header.Get("X-Github-Event"), headers, bodyBytes, r.Method, r.URL.Path,
		)

		writeJSON(w, http.StatusOK, map[string]string{"status": "received"})
	}
}

// StreamRequests provides an SSE stream of new webhook requests for an endpoint.
func StreamRequests(db *sql.DB, hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		endpointID := chi.URLParam(r, "endpointID")

		// Verify ownership.
		var ownerID int64
		err := db.QueryRow("SELECT user_id FROM webhook_endpoints WHERE id = ?", endpointID).Scan(&ownerID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
			}
			return
		}
		if ownerID != user.ID {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher.Flush()

		ch := hub.subscribe(endpointID)
		defer hub.unsubscribe(endpointID, ch)

		// Send keepalive every 30s to prevent connection timeout.
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case req := <-ch:
				data, _ := json.Marshal(req)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			case <-ticker.C:
				fmt.Fprintf(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	}
}
