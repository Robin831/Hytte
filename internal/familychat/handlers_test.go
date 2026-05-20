package familychat

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-familychat-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enable fk: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE users (
			id         INTEGER PRIMARY KEY,
			email      TEXT UNIQUE NOT NULL,
			name       TEXT NOT NULL,
			picture    TEXT NOT NULL DEFAULT '',
			google_id  TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_admin   INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE family_chat_conversations (
			id              INTEGER PRIMARY KEY,
			name            TEXT NOT NULL DEFAULT '',
			owner_user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at      TEXT NOT NULL DEFAULT '',
			last_message_at TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE family_chat_members (
			conversation_id INTEGER NOT NULL REFERENCES family_chat_conversations(id) ON DELETE CASCADE,
			user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			joined_at       TEXT NOT NULL DEFAULT '',
			last_read_at    TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (conversation_id, user_id)
		);
		CREATE TABLE family_chat_messages (
			id              INTEGER PRIMARY KEY,
			conversation_id INTEGER NOT NULL REFERENCES family_chat_conversations(id) ON DELETE CASCADE,
			sender_user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			body            TEXT NOT NULL DEFAULT '',
			attachment_path TEXT NOT NULL DEFAULT '',
			attachment_mime TEXT NOT NULL DEFAULT '',
			created_at      TEXT NOT NULL DEFAULT '',
			edited_at       TEXT,
			deleted_at      TEXT,
			deleted_by      INTEGER REFERENCES users(id) ON DELETE SET NULL
		);
		CREATE TABLE IF NOT EXISTS vapid_keys (
			id          INTEGER PRIMARY KEY,
			public_key  TEXT NOT NULL,
			private_key TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS push_subscriptions (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			endpoint   TEXT NOT NULL,
			p256dh     TEXT NOT NULL,
			auth       TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT '',
			UNIQUE(user_id, endpoint)
		);
		CREATE TABLE family_chat_message_reactions (
			message_id  INTEGER NOT NULL REFERENCES family_chat_messages(id) ON DELETE CASCADE,
			user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			emoji       TEXT NOT NULL,
			reacted_at  TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (message_id, user_id, emoji)
		);
	`); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func makeUser(t *testing.T, db *sql.DB, id int64, email string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO users (id, email, name, google_id) VALUES (?, ?, ?, ?)`,
		id, email, email, fmt.Sprintf("google-%d", id),
	); err != nil {
		t.Fatalf("insert user %d: %v", id, err)
	}
}

func withUser(r *http.Request, userID int64) *http.Request {
	user := &auth.User{ID: userID, Email: fmt.Sprintf("u%d@example.com", userID), Name: "Test"}
	return r.WithContext(auth.ContextWithUser(r.Context(), user))
}

func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// withChiParams sets multiple URL params at once. Use this when a route has
// more than one path param: withChiParam would otherwise wipe the prior call
// because each invocation creates a fresh RouteContext.
func withChiParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// seedConversation creates a conversation owned by ownerID with extra members
// listed in members. Returns the conversation ID.
func seedConversation(t *testing.T, db *sql.DB, ownerID int64, name string, members ...int64) int64 {
	t.Helper()
	c, err := CreateConversation(db, ownerID, name, members)
	if err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	return c.ID
}

func TestListConversationsHandler_OnlyOwnMemberships(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")

	// alice + bob conversation; carol is excluded.
	seedConversation(t, db, 1, "Alice/Bob", 2)
	// bob owns a chat with carol; alice should not see it.
	seedConversation(t, db, 2, "Bob/Carol", 3)

	req := withUser(httptest.NewRequest("GET", "/api/familychat/conversations", nil), 1)
	rec := httptest.NewRecorder()
	ListConversationsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Conversations []Conversation `json:"conversations"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Conversations) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(body.Conversations))
	}
	if body.Conversations[0].Name != "Alice/Bob" {
		t.Errorf("unexpected name: %q", body.Conversations[0].Name)
	}
}

func TestCreateConversationHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")

	payload := `{"name":"Family","member_user_ids":[2]}`
	req := withUser(httptest.NewRequest("POST", "/api/familychat/conversations", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateConversationHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Conversation Conversation `json:"conversation"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Conversation.Name != "Family" {
		t.Errorf("name = %q", body.Conversation.Name)
	}
	if len(body.Conversation.MemberIDs) != 2 {
		t.Errorf("expected 2 members, got %d: %v", len(body.Conversation.MemberIDs), body.Conversation.MemberIDs)
	}
}

func TestCreateConversationHandler_EmptyNameAllowed(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")

	// Empty name is valid — the schema allows it (unnamed/1:1 chats derive
	// their display name client-side).
	req := withUser(httptest.NewRequest("POST", "/api/familychat/conversations", strings.NewReader(`{"name":""}`)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateConversationHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for empty name, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetConversationHandler_MemberAndNonMember(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	// Member (bob, id=2) sees 200.
	idStr := strconv.FormatInt(convID, 10)
	req := withUser(httptest.NewRequest("GET", "/api/familychat/conversations/"+idStr, nil), 2)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	GetConversationHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("member: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Non-member (carol, id=3) sees 404.
	req = withUser(httptest.NewRequest("GET", "/api/familychat/conversations/"+idStr, nil), 3)
	req = withChiParam(req, "id", idStr)
	rec = httptest.NewRecorder()
	GetConversationHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-member: expected 404, got %d", rec.Code)
	}
}

func TestGetConversationHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")

	req := withUser(httptest.NewRequest("GET", "/api/familychat/conversations/9999", nil), 1)
	req = withChiParam(req, "id", "9999")
	rec := httptest.NewRecorder()
	GetConversationHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestPostMessageHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	idStr := strconv.FormatInt(convID, 10)
	payload := `{"body":"hello there"}`
	req := withUser(httptest.NewRequest("POST", "/api/familychat/conversations/"+idStr+"/messages", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	PostMessageHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Message Message `json:"message"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Message.Body != "hello there" {
		t.Errorf("body = %q", body.Message.Body)
	}
	if body.Message.SenderUserID != 1 {
		t.Errorf("sender = %d", body.Message.SenderUserID)
	}

	// Stored body should be encrypted (prefixed with enc:).
	var stored string
	if err := db.QueryRow(`SELECT body FROM family_chat_messages WHERE id = ?`, body.Message.ID).Scan(&stored); err != nil {
		t.Fatalf("read stored body: %v", err)
	}
	if !strings.HasPrefix(stored, "enc:") {
		t.Errorf("body not encrypted at rest: %q", stored)
	}
}

func TestPostMessageHandler_NonMember(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	idStr := strconv.FormatInt(convID, 10)
	req := withUser(httptest.NewRequest("POST", "/api/familychat/conversations/"+idStr+"/messages", strings.NewReader(`{"body":"sneaky"}`)), 3)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	PostMessageHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-member, got %d", rec.Code)
	}

	// No row should have been written.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM family_chat_messages WHERE conversation_id = ?`, convID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("non-member message persisted (%d rows)", count)
	}
}

func TestPostMessageHandler_EmptyBodyRejected(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")

	idStr := strconv.FormatInt(convID, 10)
	req := withUser(httptest.NewRequest("POST", "/api/familychat/conversations/"+idStr+"/messages", strings.NewReader(`{"body":""}`)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	PostMessageHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestPostMessageHandler_AttachmentEncryption(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")

	// Stage an attachment file under the conv's storage dir so the new
	// existence check in PostMessageHandler is satisfied.
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	dir, err := attachmentDir(convID)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	uuid := "abcdef0123456789abcdef0123456789"
	if err := os.WriteFile(filepath.Join(dir, uuid), []byte("not really a jpeg"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	// Write the .mime sidecar that UploadAttachmentHandler would normally
	// create. The server reads this instead of trusting the client's
	// attachment_mime field.
	if err := os.WriteFile(filepath.Join(dir, uuid+".mime"), []byte("image/jpeg"), 0600); err != nil {
		t.Fatalf("write mime sidecar: %v", err)
	}

	idStr := strconv.FormatInt(convID, 10)
	// Send a spoofed attachment_mime to verify the server ignores it and uses
	// the server-determined type from the .mime sidecar instead.
	payload := fmt.Sprintf(`{"body":"","attachment_path":%q,"attachment_mime":"application/pdf"}`, uuid)
	req := withUser(httptest.NewRequest("POST", "/api/familychat/conversations/"+idStr+"/messages", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	PostMessageHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Message Message `json:"message"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Response returns plaintext values.
	if body.Message.AttachmentPath != uuid {
		t.Errorf("response attachment_path = %q, want %q", body.Message.AttachmentPath, uuid)
	}
	// Server must use the sidecar MIME (image/jpeg), not the client-supplied
	// application/pdf.
	if body.Message.AttachmentMime != "image/jpeg" {
		t.Errorf("response attachment_mime = %q, want image/jpeg (server-determined)", body.Message.AttachmentMime)
	}

	// DB stores attachment_path encrypted, attachment_mime as plaintext.
	var storedPath, storedMime string
	if err := db.QueryRow(`SELECT attachment_path, attachment_mime FROM family_chat_messages WHERE id = ?`, body.Message.ID).Scan(&storedPath, &storedMime); err != nil {
		t.Fatalf("read stored row: %v", err)
	}
	if !strings.HasPrefix(storedPath, "enc:") {
		t.Errorf("attachment_path not encrypted at rest: %q", storedPath)
	}
	if storedMime != "image/jpeg" {
		t.Errorf("attachment_mime should be server-determined image/jpeg, got %q", storedMime)
	}
}


func TestListMessagesHandler_NonMember(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)
	if _, err := CreateMessage(db, convID, 1, "hi", "", ""); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	idStr := strconv.FormatInt(convID, 10)
	req := withUser(httptest.NewRequest("GET", "/api/familychat/conversations/"+idStr+"/messages", nil), 3)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	ListMessagesHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestListMessagesHandler_PaginationWithSince(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	// Seed 7 messages.
	const total = 7
	for i := 1; i <= total; i++ {
		if _, err := CreateMessage(db, convID, 1, fmt.Sprintf("msg %d", i), "", ""); err != nil {
			t.Fatalf("seed message %d: %v", i, err)
		}
	}

	idStr := strconv.FormatInt(convID, 10)

	// Page 1: limit=3, no since -> newest 3 messages, ids 7..5.
	req := withUser(httptest.NewRequest("GET", "/api/familychat/conversations/"+idStr+"/messages?limit=3", nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	ListMessagesHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("page1: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Messages []Message `json:"messages"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode page1: %v", err)
	}
	if len(body.Messages) != 3 {
		t.Fatalf("page1 len = %d", len(body.Messages))
	}
	if body.Messages[0].ID != 7 || body.Messages[2].ID != 5 {
		t.Errorf("page1 ids = %d..%d, want 7..5", body.Messages[0].ID, body.Messages[2].ID)
	}

	page1IDs := []int64{body.Messages[0].ID, body.Messages[1].ID, body.Messages[2].ID}

	// Page 2: limit=10 to fetch everything; ids should be 7..1 in order.
	req = withUser(httptest.NewRequest("GET", "/api/familychat/conversations/"+idStr+"/messages?limit=10", nil), 1)
	req = withChiParam(req, "id", idStr)
	rec = httptest.NewRecorder()
	ListMessagesHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("full: expected 200, got %d", rec.Code)
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode full: %v", err)
	}
	if len(body.Messages) != total {
		t.Fatalf("full len = %d, want %d", len(body.Messages), total)
	}
	for i, want := range []int64{7, 6, 5, 4, 3, 2, 1} {
		if body.Messages[i].ID != want {
			t.Errorf("full[%d] = %d, want %d", i, body.Messages[i].ID, want)
		}
	}

	// since filter: since=4 returns the 3 messages with id > 4 (i.e. 7,6,5).
	req = withUser(httptest.NewRequest("GET", "/api/familychat/conversations/"+idStr+"/messages?since=4&limit=10", nil), 1)
	req = withChiParam(req, "id", idStr)
	rec = httptest.NewRecorder()
	ListMessagesHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("since: expected 200, got %d", rec.Code)
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode since: %v", err)
	}
	if len(body.Messages) != 3 {
		t.Fatalf("since len = %d, want 3", len(body.Messages))
	}
	for i, want := range []int64{7, 6, 5} {
		if body.Messages[i].ID != want {
			t.Errorf("since[%d] = %d, want %d", i, body.Messages[i].ID, want)
		}
	}

	// Walking forward with `since` (incremental load scenario): the
	// first page returns newest k messages. After a new message is posted,
	// querying with since=<highest seen id> returns only the new message.
	highestSeen := page1IDs[0]
	if _, err := CreateMessage(db, convID, 1, "after page1", "", ""); err != nil {
		t.Fatalf("seed new message: %v", err)
	}
	url := fmt.Sprintf("/api/familychat/conversations/%s/messages?since=%d&limit=10", idStr, highestSeen)
	req = withUser(httptest.NewRequest("GET", url, nil), 1)
	req = withChiParam(req, "id", idStr)
	rec = httptest.NewRecorder()
	ListMessagesHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("incremental: expected 200, got %d", rec.Code)
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode incremental: %v", err)
	}
	if len(body.Messages) != 1 {
		t.Fatalf("incremental len = %d, want 1", len(body.Messages))
	}
	if body.Messages[0].Body != "after page1" {
		t.Errorf("incremental body = %q", body.Messages[0].Body)
	}
}

func TestMarkReadHandler(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	// Alice posts 3 messages; Bob should see unread_count=3 before MarkRead.
	for i := 0; i < 3; i++ {
		if _, err := CreateMessage(db, convID, 1, fmt.Sprintf("msg %d", i), "", ""); err != nil {
			t.Fatalf("seed message: %v", err)
		}
	}

	// Sanity check the unread count before MarkRead.
	c, err := GetConversation(db, convID, 2)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if c.UnreadCount != 3 {
		t.Errorf("unread before MarkRead = %d, want 3", c.UnreadCount)
	}

	idStr := strconv.FormatInt(convID, 10)

	// Bob marks read with an explicit far-future watermark so we are not
	// vulnerable to millisecond-precision ties between CreateMessage and the
	// implicit "now" in MarkRead.
	payload := `{"at":"9999-12-31T23:59:59.999Z"}`
	req := withUser(httptest.NewRequest("POST", "/api/familychat/conversations/"+idStr+"/read", strings.NewReader(payload)), 2)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	MarkReadHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Now Bob's view should show unread_count = 0.
	c, err = GetConversation(db, convID, 2)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if c.UnreadCount != 0 {
		t.Errorf("unread after MarkRead = %d, want 0", c.UnreadCount)
	}
}

func TestMarkReadHandler_NonMember(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	idStr := strconv.FormatInt(convID, 10)
	req := withUser(httptest.NewRequest("POST", "/api/familychat/conversations/"+idStr+"/read", nil), 3)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	MarkReadHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteConversationHandler_OwnerSucceeds(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	// Seed a message so cascade can be verified.
	if _, err := CreateMessage(db, convID, 1, "soon to die", "", ""); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	idStr := strconv.FormatInt(convID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/familychat/conversations/"+idStr, nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	DeleteConversationHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Conversation row, members, and messages must all be gone.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM family_chat_conversations WHERE id = ?`, convID).Scan(&n); err != nil {
		t.Fatalf("count conv: %v", err)
	}
	if n != 0 {
		t.Errorf("conversation row still present (n=%d)", n)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM family_chat_members WHERE conversation_id = ?`, convID).Scan(&n); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if n != 0 {
		t.Errorf("member rows not cascaded (n=%d)", n)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM family_chat_messages WHERE conversation_id = ?`, convID).Scan(&n); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if n != 0 {
		t.Errorf("message rows not cascaded (n=%d)", n)
	}
}

func TestDeleteConversationHandler_NonOwnerMember(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	// Bob is a member but not the owner — DELETE must 404.
	idStr := strconv.FormatInt(convID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/familychat/conversations/"+idStr, nil), 2)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	DeleteConversationHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}

	// Conversation must still exist.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM family_chat_conversations WHERE id = ?`, convID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("conversation should still exist (n=%d)", n)
	}
}

func TestCreateConversationHandler_NameTooLong(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")

	longName := strings.Repeat("a", maxNameLen+1)
	payload := fmt.Sprintf(`{"name":%q}`, longName)
	req := withUser(httptest.NewRequest("POST", "/api/familychat/conversations", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateConversationHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateConversationHandler_InvalidMemberID(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")

	payload := `{"name":"Family","member_user_ids":[0]}`
	req := withUser(httptest.NewRequest("POST", "/api/familychat/conversations", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateConversationHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateConversationHandler_NonExistentMemberID(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	// user 9999 does not exist — should get 400, not 500.
	payload := `{"name":"Family","member_user_ids":[9999]}`
	req := withUser(httptest.NewRequest("POST", "/api/familychat/conversations", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateConversationHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-existent user, got %d: %s", rec.Code, rec.Body.String())
	}
}


func TestPostMessageHandler_BodyTooLong(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")

	idStr := strconv.FormatInt(convID, 10)
	longBody := strings.Repeat("x", maxBodyLen+1)
	payload := fmt.Sprintf(`{"body":%q}`, longBody)
	req := withUser(httptest.NewRequest("POST", "/api/familychat/conversations/"+idStr+"/messages", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	PostMessageHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestListMessagesHandler_LimitTooLarge(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")

	idStr := strconv.FormatInt(convID, 10)
	url := fmt.Sprintf("/api/familychat/conversations/%s/messages?limit=%d", idStr, maxMsgLimit+1)
	req := withUser(httptest.NewRequest("GET", url, nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	ListMessagesHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestMarkReadHandler_InvalidTimestamp(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")

	idStr := strconv.FormatInt(convID, 10)
	payload := `{"at":"not-a-real-timestamp"}`
	req := withUser(httptest.NewRequest("POST", "/api/familychat/conversations/"+idStr+"/read", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	MarkReadHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}


func TestDeleteConversationHandler_NonMember(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	idStr := strconv.FormatInt(convID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/familychat/conversations/"+idStr, nil), 3)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	DeleteConversationHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestPostMessageHandler_NotifiesOnlyOfflineRecipients(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com") // sender
	makeUser(t, db, 2, "bob@example.com")   // online via SSE
	makeUser(t, db, 3, "carol@example.com") // offline
	convID := seedConversation(t, db, 1, "Family", 2, 3)

	hub := NewHub()
	// Bob is live on SSE — fan-out must skip him.
	bobSub := hub.Subscribe(2, convID)
	defer hub.Unsubscribe(convID, bobSub)
	// Drain Bob's events so the hub buffer cannot fill mid-test.
	go func() {
		for range bobSub.Events() {
		}
	}()

	type call struct {
		userID  int64
		payload []byte
	}
	var mu sync.Mutex
	var calls []call
	sender := func(userID int64, payload []byte) error {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, call{userID: userID, payload: append([]byte(nil), payload...)})
		return nil
	}

	handler := postMessageHandler(db, hub, sender, true /* notifySync */)

	idStr := strconv.FormatInt(convID, 10)
	payloadStr := `{"body":"hello family"}`
	req := withUser(httptest.NewRequest("POST", "/api/familychat/conversations/"+idStr+"/messages", strings.NewReader(payloadStr)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 push call (offline recipient), got %d", len(calls))
	}
	if calls[0].userID != 3 {
		t.Fatalf("push went to user %d, want 3 (offline recipient)", calls[0].userID)
	}

	var note push.Notification
	if err := json.Unmarshal(calls[0].payload, &note); err != nil {
		t.Fatalf("decode push payload: %v", err)
	}
	// withUser builds users with Name="Test"; that's the sender's display name.
	if note.Title != "Test" {
		t.Errorf("title = %q, want %q", note.Title, "Test")
	}
	if note.Body != "hello family" {
		t.Errorf("body = %q, want %q", note.Body, "hello family")
	}
	wantTag := fmt.Sprintf("familychat-%d", convID)
	if note.Tag != wantTag {
		t.Errorf("tag = %q, want %q", note.Tag, wantTag)
	}
}

func TestPostMessageHandler_TruncatesLongBodyInPush(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	var captured []byte
	sender := func(userID int64, payload []byte) error {
		captured = append([]byte(nil), payload...)
		return nil
	}
	handler := postMessageHandler(db, NewHub(), sender, true)

	long := strings.Repeat("a", 200)
	idStr := strconv.FormatInt(convID, 10)
	payloadStr := fmt.Sprintf(`{"body":%q}`, long)
	req := withUser(httptest.NewRequest("POST", "/api/familychat/conversations/"+idStr+"/messages", strings.NewReader(payloadStr)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if captured == nil {
		t.Fatal("push sender was not called")
	}
	var note push.Notification
	if err := json.Unmarshal(captured, &note); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len([]rune(note.Body)) > notificationBodyLimit+1 {
		t.Errorf("body rune length = %d, want at most %d (limit + ellipsis)", len([]rune(note.Body)), notificationBodyLimit+1)
	}
	if !strings.HasSuffix(note.Body, "…") {
		t.Errorf("expected ellipsis on truncated body, got %q", note.Body)
	}
}

func TestPostMessageHandler_StaleSubscriptionRemovedOn410(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	// A push endpoint that signals the subscription is gone. The internal/push
	// package is responsible for deleting the row when this happens; this test
	// verifies the family-chat fan-out exercises that path end to end.
	var hitsMu sync.Mutex
	var hits int
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsMu.Lock()
		hits++
		hitsMu.Unlock()
		w.WriteHeader(http.StatusGone)
	}))
	defer ts.Close()

	// Real P-256 keypair so the encryption step succeeds before the HTTP layer.
	curve := ecdh.P256()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	p256dh := base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
	authSecret := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	if _, err := push.SaveSubscription(db, 2, ts.URL, p256dh, authSecret); err != nil {
		t.Fatalf("save subscription: %v", err)
	}

	sender := func(userID int64, payload []byte) error {
		_, err := push.SendToUser(db, ts.Client(), userID, payload)
		return err
	}
	handler := postMessageHandler(db, NewHub(), sender, true)

	idStr := strconv.FormatInt(convID, 10)
	payloadStr := `{"body":"hi"}`
	req := withUser(httptest.NewRequest("POST", "/api/familychat/conversations/"+idStr+"/messages", strings.NewReader(payloadStr)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	hitsMu.Lock()
	gotHits := hits
	hitsMu.Unlock()
	if gotHits == 0 {
		t.Fatal("test push endpoint received no requests")
	}

	var remaining int
	if err := db.QueryRow(`SELECT COUNT(*) FROM push_subscriptions WHERE user_id = ?`, int64(2)).Scan(&remaining); err != nil {
		t.Fatalf("count subs: %v", err)
	}
	if remaining != 0 {
		t.Errorf("expected stale subscription deleted after 410 Gone, still have %d", remaining)
	}
}

func TestPostMessageHandler_SkipsPushWhenAllRecipientsOnline(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	hub := NewHub()
	bobSub := hub.Subscribe(2, convID)
	defer hub.Unsubscribe(convID, bobSub)
	go func() {
		for range bobSub.Events() {
		}
	}()

	var calls int
	sender := func(userID int64, payload []byte) error {
		calls++
		return nil
	}
	handler := postMessageHandler(db, hub, sender, true)

	idStr := strconv.FormatInt(convID, 10)
	payloadStr := `{"body":"yo"}`
	req := withUser(httptest.NewRequest("POST", "/api/familychat/conversations/"+idStr+"/messages", strings.NewReader(payloadStr)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
	if calls != 0 {
		t.Errorf("expected no push (every recipient on SSE), got %d", calls)
	}
}

func TestEditMessageHandler_AuthorEditsAndStampsEditedAt(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	original, err := CreateMessage(db, convID, 1, "first draft", "", "")
	if err != nil {
		t.Fatalf("seed message: %v", err)
	}

	hub := NewHub()
	sub := hub.Subscribe(2, convID)
	defer hub.Unsubscribe(convID, sub)

	idStr := strconv.FormatInt(convID, 10)
	msgIDStr := strconv.FormatInt(original.ID, 10)
	req := withUser(httptest.NewRequest(
		"PATCH",
		fmt.Sprintf("/api/familychat/conversations/%s/messages/%s", idStr, msgIDStr),
		strings.NewReader(`{"body":"polished"}`),
	), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"id": idStr, "messageID": msgIDStr})
	rec := httptest.NewRecorder()
	editMessageHandler(db, hub).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Message Message `json:"message"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Message.Body != "polished" {
		t.Errorf("body = %q, want polished", body.Message.Body)
	}
	if body.Message.EditedAt == nil || *body.Message.EditedAt == "" {
		t.Errorf("edited_at not stamped: %+v", body.Message.EditedAt)
	}

	// Body is re-encrypted at rest.
	var stored string
	if err := db.QueryRow(`SELECT body FROM family_chat_messages WHERE id = ?`, original.ID).Scan(&stored); err != nil {
		t.Fatalf("read stored: %v", err)
	}
	if !strings.HasPrefix(stored, "enc:") {
		t.Errorf("edited body not encrypted at rest: %q", stored)
	}

	// SSE message_edited is published to the subscriber.
	select {
	case evt := <-sub.Events():
		if evt.Type != EventMessageEdited {
			t.Errorf("event type = %q, want %q", evt.Type, EventMessageEdited)
		}
		data, ok := evt.Data.(map[string]any)
		if !ok {
			t.Fatalf("event data not map: %T", evt.Data)
		}
		if data["body"] != "polished" {
			t.Errorf("event body = %v, want polished", data["body"])
		}
	default:
		t.Fatal("no SSE message_edited event published")
	}
}

func TestEditMessageHandler_NonAuthor404(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	original, err := CreateMessage(db, convID, 1, "alice's note", "", "")
	if err != nil {
		t.Fatalf("seed message: %v", err)
	}

	idStr := strconv.FormatInt(convID, 10)
	msgIDStr := strconv.FormatInt(original.ID, 10)
	// Bob (id=2) tries to edit Alice's message.
	req := withUser(httptest.NewRequest(
		"PATCH",
		fmt.Sprintf("/api/familychat/conversations/%s/messages/%s", idStr, msgIDStr),
		strings.NewReader(`{"body":"sneaky edit"}`),
	), 2)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"id": idStr, "messageID": msgIDStr})
	rec := httptest.NewRecorder()
	EditMessageHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-author, got %d", rec.Code)
	}

	// The body must be unchanged.
	msgs, err := ListMessages(db, convID, 1, 0, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if msgs[0].Body != "alice's note" {
		t.Errorf("body changed: %q", msgs[0].Body)
	}
	if msgs[0].EditedAt != nil {
		t.Errorf("edited_at stamped despite rejected edit: %+v", msgs[0].EditedAt)
	}
}

func TestEditMessageHandler_TombstoneRejected(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")

	original, err := CreateMessage(db, convID, 1, "before delete", "", "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := SoftDeleteMessage(db, convID, original.ID, 1); err != nil {
		t.Fatalf("soft-delete: %v", err)
	}

	idStr := strconv.FormatInt(convID, 10)
	msgIDStr := strconv.FormatInt(original.ID, 10)
	req := withUser(httptest.NewRequest(
		"PATCH",
		fmt.Sprintf("/api/familychat/conversations/%s/messages/%s", idStr, msgIDStr),
		strings.NewReader(`{"body":"resurrect"}`),
	), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"id": idStr, "messageID": msgIDStr})
	rec := httptest.NewRecorder()
	EditMessageHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for tombstone, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteMessageHandler_AuthorSoftDeletePreservesRow(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	original, err := CreateMessage(db, convID, 1, "to be deleted", "", "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	hub := NewHub()
	sub := hub.Subscribe(2, convID)
	defer hub.Unsubscribe(convID, sub)

	idStr := strconv.FormatInt(convID, 10)
	msgIDStr := strconv.FormatInt(original.ID, 10)
	req := withUser(httptest.NewRequest(
		"DELETE",
		fmt.Sprintf("/api/familychat/conversations/%s/messages/%s", idStr, msgIDStr),
		nil,
	), 1)
	req = withChiParams(req, map[string]string{"id": idStr, "messageID": msgIDStr})
	rec := httptest.NewRecorder()
	deleteMessageHandler(db, hub).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Row must still exist with cleared body + populated deleted_at/deleted_by.
	var (
		body      string
		deletedAt sql.NullString
		deletedBy sql.NullInt64
	)
	if err := db.QueryRow(
		`SELECT body, deleted_at, deleted_by FROM family_chat_messages WHERE id = ?`,
		original.ID,
	).Scan(&body, &deletedAt, &deletedBy); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if body != "" {
		t.Errorf("body not cleared: %q", body)
	}
	if !deletedAt.Valid || deletedAt.String == "" {
		t.Errorf("deleted_at not set: %+v", deletedAt)
	}
	if !deletedBy.Valid || deletedBy.Int64 != 1 {
		t.Errorf("deleted_by = %+v, want 1", deletedBy)
	}

	// ListMessages still returns the tombstone with cleared body.
	msgs, err := ListMessages(db, convID, 1, 0, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 tombstone in list, got %d", len(msgs))
	}
	if msgs[0].Body != "" {
		t.Errorf("tombstone body in list = %q", msgs[0].Body)
	}
	if msgs[0].DeletedAt == nil {
		t.Errorf("DeletedAt missing on tombstone")
	}
	if msgs[0].DeletedBy == nil || *msgs[0].DeletedBy != 1 {
		t.Errorf("DeletedBy = %+v, want 1", msgs[0].DeletedBy)
	}

	// SSE message_deleted carries deleted_by.
	select {
	case evt := <-sub.Events():
		if evt.Type != EventMessageDeleted {
			t.Errorf("event type = %q, want %q", evt.Type, EventMessageDeleted)
		}
		data, ok := evt.Data.(map[string]any)
		if !ok {
			t.Fatalf("event data not map: %T", evt.Data)
		}
		if data["deleted_by"] != int64(1) {
			t.Errorf("deleted_by in event = %v, want 1", data["deleted_by"])
		}
	default:
		t.Fatal("no SSE message_deleted event published")
	}
}

func TestDeleteMessageHandler_NonAuthor404(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)

	original, err := CreateMessage(db, convID, 1, "alice's", "", "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	idStr := strconv.FormatInt(convID, 10)
	msgIDStr := strconv.FormatInt(original.ID, 10)
	req := withUser(httptest.NewRequest(
		"DELETE",
		fmt.Sprintf("/api/familychat/conversations/%s/messages/%s", idStr, msgIDStr),
		nil,
	), 2)
	req = withChiParams(req, map[string]string{"id": idStr, "messageID": msgIDStr})
	rec := httptest.NewRecorder()
	DeleteMessageHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	// Row must still be intact.
	var deletedAt sql.NullString
	if err := db.QueryRow(
		`SELECT deleted_at FROM family_chat_messages WHERE id = ?`,
		original.ID,
	).Scan(&deletedAt); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if deletedAt.Valid {
		t.Errorf("non-author delete soft-deleted the row: %+v", deletedAt)
	}
}
