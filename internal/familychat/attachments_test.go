package familychat

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// jpegMagic is the 3-byte SOI + APP0 marker prefix that http.DetectContentType
// recognises as image/jpeg.
var jpegMagic = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}

// pngMagic is the 8-byte signature that http.DetectContentType uses to
// identify image/png. Useful for tests that exercise the allow-list.
var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

func buildUploadRequest(t *testing.T, convID int64, filename, mimeType string, body []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	if mimeType != "" {
		hdr.Set("Content-Type", mimeType)
	}
	part, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	if _, err := part.Write(body); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close mw: %v", err)
	}
	idStr := strconv.FormatInt(convID, 10)
	r := httptest.NewRequest("POST", "/api/familychat/conversations/"+idStr+"/upload", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r = withChiParam(r, "id", idStr)
	return r
}

func TestUploadAttachmentHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)

	payload := append([]byte(nil), jpegMagic...)
	payload = append(payload, bytes.Repeat([]byte{0x55}, 1024)...)
	req := withUser(buildUploadRequest(t, convID, "photo.jpg", "image/jpeg", payload), 1)
	rec := httptest.NewRecorder()
	UploadAttachmentHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		UploadID string `json:"upload_id"`
		Mime     string `json:"mime"`
		Size     int64  `json:"size"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.UploadID == "" {
		t.Fatal("upload_id empty")
	}
	if body.Mime != "image/jpeg" {
		t.Errorf("mime = %q, want image/jpeg", body.Mime)
	}
	if body.Size != int64(len(payload)) {
		t.Errorf("size = %d, want %d", body.Size, len(payload))
	}
	full := filepath.Join(root, "familychat", strconv.FormatInt(convID, 10), body.UploadID)
	info, err := os.Stat(full)
	if err != nil {
		t.Fatalf("stat upload file: %v", err)
	}
	if info.Size() != int64(len(payload)) {
		t.Errorf("on-disk size = %d, want %d", info.Size(), len(payload))
	}
}

func TestUploadAttachmentHandler_RejectsTooLarge(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)

	// 11 MiB exceeds the 10 MiB cap. The MaxBytesReader trips during
	// ParseMultipartForm before the handler even sees the file body.
	big := append([]byte(nil), jpegMagic...)
	big = append(big, bytes.Repeat([]byte{0x01}, 11*1024*1024)...)
	req := withUser(buildUploadRequest(t, convID, "big.jpg", "image/jpeg", big), 1)
	rec := httptest.NewRecorder()
	UploadAttachmentHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rec.Code, rec.Body.String())
	}
	// Nothing should have been persisted on disk.
	dir := filepath.Join(root, "familychat", strconv.FormatInt(convID, 10))
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) != 0 {
		t.Errorf("expected no files persisted, found %d", len(entries))
	}
}

func TestUploadAttachmentHandler_AcceptsAudioWebm(t *testing.T) {
	// MediaRecorder voice-note uploads from Chromium arrive as audio/webm.
	// Go's http.DetectContentType only knows the EBML/Matroska container
	// signature and returns "video/webm" — it cannot tell the audio-only
	// stream apart from a video. The handler disambiguates by trusting the
	// client's Content-Type when it declares audio/webm, which is what the
	// recorder sub-task depends on.
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)

	// EBML / Matroska magic 1A 45 DF A3 followed by padding. This triggers
	// Go's "video/webm" sniff signature; the handler then overrides it to
	// "audio/webm" based on the client-supplied Content-Type.
	payload := []byte{0x1A, 0x45, 0xDF, 0xA3}
	payload = append(payload, bytes.Repeat([]byte{0x00}, 512)...)
	req := withUser(buildUploadRequest(t, convID, "voice.webm", "audio/webm", payload), 1)
	rec := httptest.NewRecorder()
	UploadAttachmentHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for audio/webm, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		UploadID string `json:"upload_id"`
		Mime     string `json:"mime"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Mime != "audio/webm" {
		t.Errorf("mime = %q, want audio/webm", body.Mime)
	}
}

func TestUploadAttachmentHandler_AcceptsAudioOgg(t *testing.T) {
	// MediaRecorder voice-note uploads from Firefox arrive as audio/ogg.
	// Go's sniff table recognises the OggS magic and returns application/ogg,
	// so the allowlist must accept that variant — adding only "audio/ogg"
	// would silently reject real OGG bodies. Use OggS-prefixed payload to
	// exercise the sniff-then-allowlist path end to end.
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)

	payload := append([]byte("OggS"), bytes.Repeat([]byte{0x00}, 512)...)
	req := withUser(buildUploadRequest(t, convID, "voice.ogg", "audio/ogg", payload), 1)
	rec := httptest.NewRecorder()
	UploadAttachmentHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for audio/ogg, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		UploadID string `json:"upload_id"`
		Mime     string `json:"mime"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Server uses the sniffed type. http.DetectContentType returns
	// application/ogg for OggS-prefixed bodies; the allowlist accepts both
	// application/ogg and audio/ogg so this is the value clients receive.
	if body.Mime != "application/ogg" && body.Mime != "audio/ogg" {
		t.Errorf("mime = %q, want application/ogg or audio/ogg", body.Mime)
	}
}

func TestUploadAttachmentHandler_RejectsUnknownAudio(t *testing.T) {
	// audio/x-aiff is deliberately outside the allowlist — only the
	// MediaRecorder-friendly audio types are accepted. Body bytes are
	// arbitrary so the sniff returns application/octet-stream and the
	// handler falls back to the client header before checking the allowlist.
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)

	payload := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	req := withUser(buildUploadRequest(t, convID, "voice.aiff", "audio/x-aiff", payload), 1)
	rec := httptest.NewRecorder()
	UploadAttachmentHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for audio/x-aiff, got %d: %s", rec.Code, rec.Body.String())
	}
	dir := filepath.Join(root, "familychat", strconv.FormatInt(convID, 10))
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected no files persisted, found %d", len(entries))
	}
}

func TestUploadAttachmentHandler_RejectsBadMime(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)

	// Plain text is not in the allowlist; the handler must reject before
	// touching disk.
	payload := []byte("just a text file, not an image\nshould be rejected\n")
	req := withUser(buildUploadRequest(t, convID, "note.txt", "text/plain", payload), 1)
	rec := httptest.NewRecorder()
	UploadAttachmentHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	dir := filepath.Join(root, "familychat", strconv.FormatInt(convID, 10))
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected no files persisted, found %d", len(entries))
	}
}

func TestUploadAttachmentHandler_NonMember(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family")
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)

	payload := append([]byte(nil), pngMagic...)
	req := withUser(buildUploadRequest(t, convID, "x.png", "image/png", payload), 2)
	rec := httptest.NewRecorder()
	UploadAttachmentHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-member, got %d", rec.Code)
	}
}

func TestGetAttachmentHandler_Member(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)

	// Stage a file under the conv's storage dir.
	dir, err := attachmentDir(convID)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	uuid := "11223344556677889900aabbccddeeff"
	content := append([]byte(nil), pngMagic...)
	content = append(content, []byte("payload-body")...)
	if err := os.WriteFile(filepath.Join(dir, uuid), content, 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	msg, err := CreateMessage(db, convID, 1, "", uuid, "image/png")
	if err != nil {
		t.Fatalf("create msg: %v", err)
	}

	// Bob (member) can fetch.
	idStr := strconv.FormatInt(convID, 10)
	r := httptest.NewRequest("GET", "/api/familychat/conversations/"+idStr+"/attachments/"+strconv.FormatInt(msg.ID, 10), nil)
	r = withChiParams(r, map[string]string{"id": idStr, "message_id": strconv.FormatInt(msg.ID, 10)})
	r = withUser(r, 2)
	rec := httptest.NewRecorder()
	GetAttachmentHandler(db).ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
	got, _ := io.ReadAll(rec.Body)
	if !bytes.Equal(got, content) {
		t.Errorf("body mismatch: got %d bytes, want %d", len(got), len(content))
	}
}

func TestGetAttachmentHandler_NonMember(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	makeUser(t, db, 2, "bob@example.com")
	makeUser(t, db, 3, "carol@example.com")
	convID := seedConversation(t, db, 1, "Family", 2)
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)

	dir, err := attachmentDir(convID)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	uuid := "aaaabbbbccccddddeeeeffff00001111"
	if err := os.WriteFile(filepath.Join(dir, uuid), []byte("secret bytes"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	msg, err := CreateMessage(db, convID, 1, "", uuid, "image/png")
	if err != nil {
		t.Fatalf("create msg: %v", err)
	}

	// Carol is not a member — must 404.
	idStr := strconv.FormatInt(convID, 10)
	r := httptest.NewRequest("GET", "/api/familychat/conversations/"+idStr+"/attachments/"+strconv.FormatInt(msg.ID, 10), nil)
	r = withChiParams(r, map[string]string{"id": idStr, "message_id": strconv.FormatInt(msg.ID, 10)})
	r = withUser(r, 3)
	rec := httptest.NewRecorder()
	GetAttachmentHandler(db).ServeHTTP(rec, r)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-member, got %d", rec.Code)
	}
}

func TestGetAttachmentHandler_NoAttachment(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")
	t.Setenv("UPLOAD_ROOT", t.TempDir())

	msg, err := CreateMessage(db, convID, 1, "text only", "", "")
	if err != nil {
		t.Fatalf("create msg: %v", err)
	}

	idStr := strconv.FormatInt(convID, 10)
	r := httptest.NewRequest("GET", "/api/familychat/conversations/"+idStr+"/attachments/"+strconv.FormatInt(msg.ID, 10), nil)
	r = withChiParams(r, map[string]string{"id": idStr, "message_id": strconv.FormatInt(msg.ID, 10)})
	r = withUser(r, 1)
	rec := httptest.NewRecorder()
	GetAttachmentHandler(db).ServeHTTP(rec, r)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestResolveAttachmentPath_RejectsTraversal(t *testing.T) {
	t.Setenv("UPLOAD_ROOT", t.TempDir())
	cases := []string{
		"../escape",
		"sub/dir",
		`back\slash`,
		"..",
		".",
		"",
		"   ",
	}
	for _, c := range cases {
		if _, ok := resolveAttachmentPath(42, c); ok {
			t.Errorf("expected %q to be rejected", c)
		}
	}
	// A flat UUID-shaped filename is accepted.
	if _, ok := resolveAttachmentPath(42, "abcdef0123456789abcdef0123456789"); !ok {
		t.Error("expected flat uuid to be accepted")
	}
}

func TestPostMessageHandler_RejectsFakeAttachment(t *testing.T) {
	db := setupTestDB(t)
	makeUser(t, db, 1, "alice@example.com")
	convID := seedConversation(t, db, 1, "Family")
	t.Setenv("UPLOAD_ROOT", t.TempDir())

	// Claim an attachment without actually having uploaded one.
	idStr := strconv.FormatInt(convID, 10)
	payload := `{"body":"","attachment_path":"deadbeefdeadbeefdeadbeefdeadbeef","attachment_mime":"image/jpeg"}`
	r := httptest.NewRequest("POST", "/api/familychat/conversations/"+idStr+"/messages", strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	r = withChiParam(r, "id", idStr)
	r = withUser(r, 1)
	rec := httptest.NewRecorder()
	PostMessageHandler(db).ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing attachment, got %d", rec.Code)
	}
}
