package pokemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// buildQueueRequest is a multipart upload builder for /api/pokemon/scans/queue.
// The handler sniffs bytes for MIME, not the filename — passing a JPEG header
// is enough to satisfy the validation regardless of the file extension.
func buildQueueRequest(t *testing.T, payload []byte, filename string) *http.Request {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, err := w.CreateFormFile("image", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatalf("write form payload: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/pokemon/scans/queue", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// readScanJobByID reads the persisted row for assertions. Returns the encrypted
// image path verbatim so tests can decrypt + stat to verify file deletion.
func readScanJobByID(t *testing.T, db *sql.DB, jobID int64) (status string, pathEnc string, errMsg sql.NullString, resolvedAt sql.NullTime, matchedCard sql.NullString, matchedVariant sql.NullInt64) {
	t.Helper()
	if err := db.QueryRow(`
		SELECT status, image_path_enc, error_message, resolved_at, matched_card_id, matched_variant_id
		FROM pokemon_scan_jobs WHERE id = ?
	`, jobID).Scan(&status, &pathEnc, &errMsg, &resolvedAt, &matchedCard, &matchedVariant); err != nil {
		t.Fatalf("read scan job %d: %v", jobID, err)
	}
	return
}

func TestQueueScan_HappyPath(t *testing.T) {
	t.Setenv("UPLOAD_ROOT", t.TempDir())
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "queue@example.com")

	payload := append([]byte{}, jpegMagic...)
	payload = append(payload, []byte("payload-bytes")...)

	req := asUser(buildQueueRequest(t, payload, "card.jpg"), u)
	rec := httptest.NewRecorder()
	QueueScanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}](t, rec)
	if body.ID == 0 || body.Status != "queued" {
		t.Fatalf("unexpected body: %+v", body)
	}

	status, pathEnc, _, _, _, _ := readScanJobByID(t, db, body.ID)
	if status != scanJobStatusQueued {
		t.Errorf("expected status=queued, got %q", status)
	}
	if pathEnc == "" {
		t.Fatalf("expected image_path_enc to be set")
	}
	path, err := encryption.DecryptField(pathEnc)
	if err != nil {
		t.Fatalf("decrypt path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected image on disk at %s: %v", path, err)
	}

	var imageHash string
	if err := db.QueryRow(`SELECT image_hash FROM pokemon_scan_jobs WHERE id = ?`, body.ID).Scan(&imageHash); err != nil {
		t.Fatalf("read image_hash: %v", err)
	}
	if imageHash == "" {
		t.Errorf("expected image_hash to be populated")
	}
}

func TestQueueScan_RejectsBadMIME(t *testing.T) {
	t.Setenv("UPLOAD_ROOT", t.TempDir())
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "queue@example.com")

	req := asUser(buildQueueRequest(t, []byte("definitely not an image"), "card.jpg"), u)
	rec := httptest.NewRecorder()
	QueueScanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestQueueScan_RejectsOversize(t *testing.T) {
	t.Setenv("UPLOAD_ROOT", t.TempDir())
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "queue@example.com")

	payload := append([]byte{}, jpegMagic...)
	for len(payload) <= scanMaxImageBytes {
		payload = append(payload, 0x00)
	}
	req := asUser(buildQueueRequest(t, payload, "huge.jpg"), u)
	rec := httptest.NewRecorder()
	QueueScanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestQueueScan_MissingImage(t *testing.T) {
	t.Setenv("UPLOAD_ROOT", t.TempDir())
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "queue@example.com")

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	if err := w.WriteField("other", "value"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/pokemon/scans/queue", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req = asUser(req, u)

	rec := httptest.NewRecorder()
	QueueScanHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// insertScanJob is a list/resolve-test helper that seeds a row in any state.
// Tests pass an absolute path so HasImage assertions reflect a real file.
func insertScanJob(t *testing.T, db *sql.DB, userID int64, status, imagePath string, opts ...func(*scanJobInsert)) int64 {
	t.Helper()
	enc := ""
	if imagePath != "" {
		var err error
		enc, err = encryption.EncryptField(imagePath)
		if err != nil {
			t.Fatalf("encrypt path: %v", err)
		}
	}
	cfg := scanJobInsert{
		userID:    userID,
		status:    status,
		pathEnc:   enc,
		imageHash: "deadbeef",
		createdAt: time.Now().UTC(),
	}
	for _, o := range opts {
		o(&cfg)
	}
	respEnc := ""
	if cfg.claudeResponse != "" {
		var err error
		respEnc, err = encryption.EncryptField(cfg.claudeResponse)
		if err != nil {
			t.Fatalf("encrypt claude response: %v", err)
		}
	}
	res, err := db.Exec(`
		INSERT INTO pokemon_scan_jobs (user_id, status, image_path_enc, image_hash, matched_card_id,
			confidence, error_message, created_at, processed_at, claude_response_enc)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, cfg.userID, cfg.status, cfg.pathEnc, cfg.imageHash, nullableString(cfg.matchedCard),
		nullableFloat(cfg.confidence), nullableString(cfg.errorMessage), cfg.createdAt,
		nullableTime(cfg.processedAt), nullableString(respEnc))
	if err != nil {
		t.Fatalf("insert scan job: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last id: %v", err)
	}
	return id
}

type scanJobInsert struct {
	userID         int64
	status         string
	pathEnc        string
	imageHash      string
	matchedCard    string
	confidence     float64
	errorMessage   string
	createdAt      time.Time
	processedAt    time.Time
	claudeResponse string
}

// withClaudeResponse stores the raw Claude JSON the worker captured so the
// list endpoint can surface the parsed partial info on no_match rows.
func withClaudeResponse(raw string) func(*scanJobInsert) {
	return func(s *scanJobInsert) { s.claudeResponse = raw }
}

func withMatchedCard(id string, confidence float64) func(*scanJobInsert) {
	return func(s *scanJobInsert) {
		s.matchedCard = id
		s.confidence = confidence
		s.processedAt = time.Now().UTC()
	}
}

func withCreatedAt(ts time.Time) func(*scanJobInsert) {
	return func(s *scanJobInsert) { s.createdAt = ts }
}

func withError(msg string) func(*scanJobInsert) {
	return func(s *scanJobInsert) {
		s.errorMessage = msg
		s.processedAt = time.Now().UTC()
	}
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

func nullableTime(ts time.Time) any {
	if ts.IsZero() {
		return nil
	}
	return ts
}

// writeOnDiskImage drops a file at the requested path so HasImage / image
// streaming tests run against a real disk artifact. The bytes start with a
// real JPEG SOI marker so http.DetectContentType (used by the image handler
// to label the response Content-Type) classifies them as image/jpeg.
func writeOnDiskImage(t *testing.T, root string, userID int64, name string) string {
	t.Helper()
	dir := filepath.Join(root, "pokemon-scans", strconv.FormatInt(userID, 10))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, name)
	jpegBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00}
	if err := os.WriteFile(path, jpegBytes, 0600); err != nil {
		t.Fatalf("write image: %v", err)
	}
	return path
}

func TestListScans_NewestFirst_DefaultFilter(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "list@example.com")

	now := time.Now().UTC()
	p1 := writeOnDiskImage(t, root, u.ID, "older.jpg")
	p2 := writeOnDiskImage(t, root, u.ID, "newer.jpg")
	older := insertScanJob(t, db, u.ID, scanJobStatusQueued, p1, withCreatedAt(now.Add(-2*time.Hour)))
	newer := insertScanJob(t, db, u.ID, scanJobStatusMatched, p2, withCreatedAt(now), withMatchedCard("sv1-25", 0.9))
	// An "added" row should not appear in the default filter.
	insertScanJob(t, db, u.ID, scanJobStatusAdded, "", withCreatedAt(now.Add(-1*time.Hour)))

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/scans", nil), u)
	rec := httptest.NewRecorder()
	ListScansHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Scans []ScanJobDTO `json:"scans"`
	}](t, rec)
	if len(body.Scans) != 2 {
		t.Fatalf("expected 2 visible scans, got %d", len(body.Scans))
	}
	if body.Scans[0].ID != newer || body.Scans[1].ID != older {
		t.Errorf("expected newest-first order, got [%d, %d]", body.Scans[0].ID, body.Scans[1].ID)
	}
	if !body.Scans[0].HasImage {
		t.Errorf("expected has_image=true for newer scan")
	}
	if body.Scans[0].MatchedCard == nil || body.Scans[0].MatchedCard.ID != "sv1-25" {
		t.Errorf("expected matched card to be hydrated, got %+v", body.Scans[0].MatchedCard)
	}
}

func TestListScans_NoMatchExposesParsedHints(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "hints@example.com")

	p := writeOnDiskImage(t, root, u.ID, "no-match.jpg")
	rawResp := `{"set_name":"Scarlet & Violet Base","set_id_hint":"sv1","collector_number":"055","confidence":0.42}`
	jobID := insertScanJob(t, db, u.ID, scanJobStatusNoMatch, p,
		withError("no candidate found"),
		withClaudeResponse(rawResp))

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/scans?status=no_match", nil), u)
	rec := httptest.NewRecorder()
	ListScansHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Scans []ScanJobDTO `json:"scans"`
	}](t, rec)
	if len(body.Scans) != 1 || body.Scans[0].ID != jobID {
		t.Fatalf("expected single no_match row, got %+v", body.Scans)
	}
	got := body.Scans[0]
	if got.ParsedSetName != "Scarlet & Violet Base" {
		t.Errorf("expected parsed_set_name to be surfaced, got %q", got.ParsedSetName)
	}
	if got.ParsedCollectorNo != "055" {
		t.Errorf("expected parsed_collector_no to be surfaced, got %q", got.ParsedCollectorNo)
	}
}

func TestListScans_MatchedDoesNotExposeHints(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "matched-hints@example.com")

	p := writeOnDiskImage(t, root, u.ID, "matched.jpg")
	rawResp := `{"set_name":"Scarlet & Violet Base","collector_number":"025","confidence":0.95}`
	insertScanJob(t, db, u.ID, scanJobStatusMatched, p,
		withMatchedCard("sv1-25", 0.95),
		withClaudeResponse(rawResp))

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/scans?status=matched", nil), u)
	rec := httptest.NewRecorder()
	ListScansHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := decode[struct {
		Scans []ScanJobDTO `json:"scans"`
	}](t, rec)
	if len(body.Scans) != 1 {
		t.Fatalf("expected one matched scan, got %d", len(body.Scans))
	}
	if body.Scans[0].ParsedSetName != "" || body.Scans[0].ParsedCollectorNo != "" {
		t.Errorf("hints should only appear on no_match rows, got set=%q collector=%q",
			body.Scans[0].ParsedSetName, body.Scans[0].ParsedCollectorNo)
	}
}

func TestListScans_NoMatchWithCorruptResponse_OmitsHints(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "corrupt-hints@example.com")

	p := writeOnDiskImage(t, root, u.ID, "corrupt.jpg")
	// Garbage that parseClaudeScanResult will fail to unmarshal — the handler
	// must still respond 200 with an empty hint pair rather than blowing up.
	insertScanJob(t, db, u.ID, scanJobStatusNoMatch, p,
		withError("low confidence"),
		withClaudeResponse("not valid json"))

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/scans?status=no_match", nil), u)
	rec := httptest.NewRecorder()
	ListScansHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Scans []ScanJobDTO `json:"scans"`
	}](t, rec)
	if len(body.Scans) != 1 {
		t.Fatalf("expected one no_match scan, got %d", len(body.Scans))
	}
	if body.Scans[0].ParsedSetName != "" || body.Scans[0].ParsedCollectorNo != "" {
		t.Errorf("expected empty hints when claude_response_enc is corrupt, got set=%q collector=%q",
			body.Scans[0].ParsedSetName, body.Scans[0].ParsedCollectorNo)
	}
}

func TestListScans_StatusFilter(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "filter@example.com")

	p := writeOnDiskImage(t, root, u.ID, "queued.jpg")
	queued := insertScanJob(t, db, u.ID, scanJobStatusQueued, p)
	insertScanJob(t, db, u.ID, scanJobStatusMatched, "", withMatchedCard("sv1-25", 0.9))

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/scans?status=queued", nil), u)
	rec := httptest.NewRecorder()
	ListScansHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := decode[struct {
		Scans []ScanJobDTO `json:"scans"`
	}](t, rec)
	if len(body.Scans) != 1 || body.Scans[0].ID != queued {
		t.Fatalf("expected single queued scan, got %+v", body.Scans)
	}
}

func TestListScans_LimitCap(t *testing.T) {
	t.Setenv("UPLOAD_ROOT", t.TempDir())
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "limit@example.com")

	for i := 0; i < 3; i++ {
		insertScanJob(t, db, u.ID, scanJobStatusQueued, "")
	}
	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/scans?limit=2", nil), u)
	rec := httptest.NewRecorder()
	ListScansHandler(db).ServeHTTP(rec, req)
	body := decode[struct {
		Scans []ScanJobDTO `json:"scans"`
	}](t, rec)
	if len(body.Scans) != 2 {
		t.Errorf("expected limit=2 to cap at 2, got %d", len(body.Scans))
	}
}

func TestListScans_CrossUserIsolation(t *testing.T) {
	t.Setenv("UPLOAD_ROOT", t.TempDir())
	db := setupTestDB(t)
	uA := seedUser(t, db, 1, "a@example.com")
	uB := seedUser(t, db, 2, "b@example.com")
	insertScanJob(t, db, uA.ID, scanJobStatusQueued, "")
	bID := insertScanJob(t, db, uB.ID, scanJobStatusQueued, "")

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/scans", nil), uB)
	rec := httptest.NewRecorder()
	ListScansHandler(db).ServeHTTP(rec, req)
	body := decode[struct {
		Scans []ScanJobDTO `json:"scans"`
	}](t, rec)
	if len(body.Scans) != 1 || body.Scans[0].ID != bID {
		t.Errorf("expected only user B's scan, got %+v", body.Scans)
	}
}

func TestGetScanImage_HappyPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "img@example.com")
	path := writeOnDiskImage(t, root, u.ID, "scan.jpg")
	id := insertScanJob(t, db, u.ID, scanJobStatusQueued, path)

	req := asChi(asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/scans/"+strconv.FormatInt(id, 10)+"/image", nil), u),
		map[string]string{"id": strconv.FormatInt(id, 10)})
	rec := httptest.NewRecorder()
	GetScanImageHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("expected Content-Type=image/jpeg, got %q", got)
	}
	if rec.Body.Len() == 0 {
		t.Errorf("expected image bytes in response")
	}
}

func TestGetScanImage_OtherUser_404(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	uA := seedUser(t, db, 1, "a@example.com")
	uB := seedUser(t, db, 2, "b@example.com")
	path := writeOnDiskImage(t, root, uA.ID, "scan.jpg")
	id := insertScanJob(t, db, uA.ID, scanJobStatusQueued, path)

	req := asChi(asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/scans/"+strconv.FormatInt(id, 10)+"/image", nil), uB),
		map[string]string{"id": strconv.FormatInt(id, 10)})
	rec := httptest.NewRecorder()
	GetScanImageHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetScanImage_MissingFile_404(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "img@example.com")
	id := insertScanJob(t, db, u.ID, scanJobStatusDiscarded, "")

	req := asChi(asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/scans/"+strconv.FormatInt(id, 10)+"/image", nil), u),
		map[string]string{"id": strconv.FormatInt(id, 10)})
	rec := httptest.NewRecorder()
	GetScanImageHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestResolveScan_Add_PersistsAndDeletesImage(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "resolve@example.com")
	path := writeOnDiskImage(t, root, u.ID, "matched.jpg")
	jobID := insertScanJob(t, db, u.ID, scanJobStatusMatched, path, withMatchedCard("sv1-25", 0.92))
	vid := variantID(t, db, "sv1-25", "normal")

	rec := callJSON(t, http.MethodPost, "/api/pokemon/scans/"+strconv.FormatInt(jobID, 10)+"/resolve",
		map[string]any{"action": "add", "variant_id": vid, "quantity": 1, "condition": "near_mint", "notes": "from scan"},
		u, map[string]string{"id": strconv.FormatInt(jobID, 10)}, ResolveScanHandler(db))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Collection row should now exist for this variant.
	var qty int
	if err := db.QueryRow(`SELECT quantity FROM pokemon_collections WHERE user_id = ? AND variant_id = ?`,
		u.ID, vid).Scan(&qty); err != nil {
		t.Fatalf("expected collection row: %v", err)
	}
	if qty != 1 {
		t.Errorf("expected quantity=1, got %d", qty)
	}

	// Job should be 'added' and the image deleted.
	status, pathEnc, _, resolvedAt, _, matchedVariant := readScanJobByID(t, db, jobID)
	if status != scanJobStatusAdded {
		t.Errorf("expected status=added, got %q", status)
	}
	if !matchedVariant.Valid || matchedVariant.Int64 != vid {
		t.Errorf("expected matched_variant_id to be set, got %+v", matchedVariant)
	}
	if !resolvedAt.Valid {
		t.Errorf("expected resolved_at to be set")
	}
	if pathEnc != "" {
		t.Errorf("expected image_path_enc to be cleared, got %q", pathEnc)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected image file to be deleted: stat err=%v", err)
	}
}

func TestResolveScan_Discard_DeletesImage(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "discard@example.com")
	path := writeOnDiskImage(t, root, u.ID, "discard.jpg")
	jobID := insertScanJob(t, db, u.ID, scanJobStatusMatched, path, withMatchedCard("sv1-25", 0.9))

	rec := callJSON(t, http.MethodPost, "/api/pokemon/scans/"+strconv.FormatInt(jobID, 10)+"/resolve",
		map[string]any{"action": "discard"}, u,
		map[string]string{"id": strconv.FormatInt(jobID, 10)}, ResolveScanHandler(db))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	status, _, _, resolvedAt, _, _ := readScanJobByID(t, db, jobID)
	if status != scanJobStatusDiscarded {
		t.Errorf("expected status=discarded, got %q", status)
	}
	if !resolvedAt.Valid {
		t.Errorf("expected resolved_at to be set")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected image file to be deleted: stat err=%v", err)
	}
}

func TestResolveScan_Retry_OnFailedRequeues(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "retry@example.com")
	path := writeOnDiskImage(t, root, u.ID, "retry.jpg")
	jobID := insertScanJob(t, db, u.ID, scanJobStatusFailed, path, withError("claude crashed"))

	rec := callJSON(t, http.MethodPost, "/api/pokemon/scans/"+strconv.FormatInt(jobID, 10)+"/resolve",
		map[string]any{"action": "retry"}, u,
		map[string]string{"id": strconv.FormatInt(jobID, 10)}, ResolveScanHandler(db))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	status, _, errMsg, _, _, _ := readScanJobByID(t, db, jobID)
	if status != scanJobStatusQueued {
		t.Errorf("expected status=queued after retry, got %q", status)
	}
	if errMsg.Valid {
		t.Errorf("expected error_message to be cleared, got %q", errMsg.String)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected image to be retained on retry, got %v", err)
	}
}

func TestResolveScan_Retry_RejectedOnMatched(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "retry@example.com")
	path := writeOnDiskImage(t, root, u.ID, "matched.jpg")
	jobID := insertScanJob(t, db, u.ID, scanJobStatusMatched, path, withMatchedCard("sv1-25", 0.9))

	rec := callJSON(t, http.MethodPost, "/api/pokemon/scans/"+strconv.FormatInt(jobID, 10)+"/resolve",
		map[string]any{"action": "retry"}, u,
		map[string]string{"id": strconv.FormatInt(jobID, 10)}, ResolveScanHandler(db))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestResolveScan_Add_RejectedOnNonMatched(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "add@example.com")
	path := writeOnDiskImage(t, root, u.ID, "queued.jpg")
	jobID := insertScanJob(t, db, u.ID, scanJobStatusQueued, path)
	vid := variantID(t, db, "sv1-25", "normal")

	rec := callJSON(t, http.MethodPost, "/api/pokemon/scans/"+strconv.FormatInt(jobID, 10)+"/resolve",
		map[string]any{"action": "add", "variant_id": vid, "quantity": 1}, u,
		map[string]string{"id": strconv.FormatInt(jobID, 10)}, ResolveScanHandler(db))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestResolveScan_CrossUser_404(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	uA := seedUser(t, db, 1, "a@example.com")
	uB := seedUser(t, db, 2, "b@example.com")
	path := writeOnDiskImage(t, root, uA.ID, "scan.jpg")
	jobID := insertScanJob(t, db, uA.ID, scanJobStatusMatched, path, withMatchedCard("sv1-25", 0.9))

	rec := callJSON(t, http.MethodPost, "/api/pokemon/scans/"+strconv.FormatInt(jobID, 10)+"/resolve",
		map[string]any{"action": "discard"}, uB,
		map[string]string{"id": strconv.FormatInt(jobID, 10)}, ResolveScanHandler(db))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestParseScanStatusFilter_Defaults(t *testing.T) {
	got := parseScanStatusFilter("")
	if len(got) != len(defaultScanListStatuses) {
		t.Fatalf("expected %d defaults, got %d", len(defaultScanListStatuses), len(got))
	}
}

func TestParseScanStatusFilter_DropsUnknownAndDedupes(t *testing.T) {
	got := parseScanStatusFilter("queued, garbage ,QUEUED,matched")
	if len(got) != 2 {
		t.Fatalf("expected 2 unique allowed entries, got %v", got)
	}
	seen := map[string]bool{}
	for _, s := range got {
		seen[s] = true
	}
	if !seen["queued"] || !seen["matched"] {
		t.Errorf("expected queued and matched, got %v", got)
	}
}

// TestResolveScan_RoundTripsMatchedCardInResponse asserts that the resolve
// response includes the matched card details so the UI can confirm what the
// user just acted on without an extra GET.
func TestResolveScan_RoundTripsMatchedCardInResponse(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "trip@example.com")
	path := writeOnDiskImage(t, root, u.ID, "card.jpg")
	jobID := insertScanJob(t, db, u.ID, scanJobStatusMatched, path, withMatchedCard("sv1-25", 0.9))
	vid := variantID(t, db, "sv1-25", "normal")

	rec := callJSON(t, http.MethodPost, "/api/pokemon/scans/"+strconv.FormatInt(jobID, 10)+"/resolve",
		map[string]any{"action": "add", "variant_id": vid, "quantity": 1}, u,
		map[string]string{"id": strconv.FormatInt(jobID, 10)}, ResolveScanHandler(db))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Scan ScanJobDTO `json:"scan"`
	}](t, rec)
	if body.Scan.Status != scanJobStatusAdded {
		t.Errorf("expected status=added in response, got %q", body.Scan.Status)
	}
	if body.Scan.MatchedCard == nil || body.Scan.MatchedCard.ID != "sv1-25" {
		t.Errorf("expected matched card in response, got %+v", body.Scan.MatchedCard)
	}
	if body.Scan.HasImage {
		t.Errorf("expected has_image=false after add (file deleted)")
	}
}

// TestUpsertCollection_HelperExposesSentinels guards the contract between
// the scan resolve path and the shared collection-upsert logic: callers rely
// on errVariantNotFound / errVariantMismatch / errInvalidCondition to render
// the right status codes.
func TestUpsertCollection_HelperExposesSentinels(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "helper@example.com")
	ctx := context.Background()

	if _, err := upsertCollection(ctx, db, u.ID, "sv1-25", 99999, 1, "", ""); !errors.Is(err, errVariantNotFound) {
		t.Errorf("expected errVariantNotFound, got %v", err)
	}

	correct := variantID(t, db, "sv1-25", "normal")
	if _, err := upsertCollection(ctx, db, u.ID, "sv1-100", correct, 1, "", ""); !errors.Is(err, errVariantMismatch) {
		t.Errorf("expected errVariantMismatch, got %v", err)
	}

	if _, err := upsertCollection(ctx, db, u.ID, "sv1-25", correct, 1, "bogus", ""); !errors.Is(err, errInvalidCondition) {
		t.Errorf("expected errInvalidCondition, got %v", err)
	}

	if _, err := upsertCollection(ctx, db, u.ID, "sv1-25", correct, -3, "", ""); !errors.Is(err, errInvalidQuantity) {
		t.Errorf("expected errInvalidQuantity, got %v", err)
	}

	isNew, err := upsertCollection(ctx, db, u.ID, "sv1-25", correct, 1, "near_mint", "")
	if err != nil {
		t.Fatalf("unexpected error on happy path: %v", err)
	}
	if !isNew {
		t.Errorf("expected isNew=true on first insert")
	}
}

// TestResolveScan_Retry_OnNoMatchRequeues verifies that a no_match scan can be
// retried just like a failed one — both are the two allowed retry sources.
func TestResolveScan_Retry_OnNoMatchRequeues(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "retry@example.com")
	path := writeOnDiskImage(t, root, u.ID, "retry.jpg")
	jobID := insertScanJob(t, db, u.ID, scanJobStatusNoMatch, path, withError("no match found"))

	rec := callJSON(t, http.MethodPost, "/api/pokemon/scans/"+strconv.FormatInt(jobID, 10)+"/resolve",
		map[string]any{"action": "retry"}, u,
		map[string]string{"id": strconv.FormatInt(jobID, 10)}, ResolveScanHandler(db))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	status, _, errMsg, _, _, _ := readScanJobByID(t, db, jobID)
	if status != scanJobStatusQueued {
		t.Errorf("expected status=queued after retry from no_match, got %q", status)
	}
	if errMsg.Valid {
		t.Errorf("expected error_message to be cleared, got %q", errMsg.String)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected image to be retained on retry, got %v", err)
	}
}

// TestResolveScan_Add_UpsertFailures checks that collection-upsert sentinel
// errors surface as the right HTTP status codes through the handler.
func TestResolveScan_Add_UpsertFailures(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "fail@example.com")
	path := writeOnDiskImage(t, root, u.ID, "card.jpg")

	correctVid := variantID(t, db, "sv1-25", "normal")
	wrongVid := variantID(t, db, "sv1-100", "normal")

	t.Run("variant not found → 404", func(t *testing.T) {
		jobID := insertScanJob(t, db, u.ID, scanJobStatusMatched, path, withMatchedCard("sv1-25", 0.9))
		rec := callJSON(t, http.MethodPost, "/api/pokemon/scans/"+strconv.FormatInt(jobID, 10)+"/resolve",
			map[string]any{"action": "add", "variant_id": int64(99999), "quantity": 1}, u,
			map[string]string{"id": strconv.FormatInt(jobID, 10)}, ResolveScanHandler(db))
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404 for unknown variant, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("variant mismatch → 400", func(t *testing.T) {
		jobID := insertScanJob(t, db, u.ID, scanJobStatusMatched, path, withMatchedCard("sv1-25", 0.9))
		rec := callJSON(t, http.MethodPost, "/api/pokemon/scans/"+strconv.FormatInt(jobID, 10)+"/resolve",
			map[string]any{"action": "add", "variant_id": wrongVid, "quantity": 1}, u,
			map[string]string{"id": strconv.FormatInt(jobID, 10)}, ResolveScanHandler(db))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for variant mismatch, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("invalid condition → 400", func(t *testing.T) {
		jobID := insertScanJob(t, db, u.ID, scanJobStatusMatched, path, withMatchedCard("sv1-25", 0.9))
		rec := callJSON(t, http.MethodPost, "/api/pokemon/scans/"+strconv.FormatInt(jobID, 10)+"/resolve",
			map[string]any{"action": "add", "variant_id": correctVid, "quantity": 1, "condition": "perfect"}, u,
			map[string]string{"id": strconv.FormatInt(jobID, 10)}, ResolveScanHandler(db))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid condition, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("invalid quantity → 400", func(t *testing.T) {
		jobID := insertScanJob(t, db, u.ID, scanJobStatusMatched, path, withMatchedCard("sv1-25", 0.9))
		rec := callJSON(t, http.MethodPost, "/api/pokemon/scans/"+strconv.FormatInt(jobID, 10)+"/resolve",
			map[string]any{"action": "add", "variant_id": correctVid, "quantity": -1}, u,
			map[string]string{"id": strconv.FormatInt(jobID, 10)}, ResolveScanHandler(db))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for negative quantity, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// TestResolveScan_Discard_ConflictsOnProcessing verifies that discarding a
// processing scan returns 409 — the worker may still be finalising the job.
func TestResolveScan_Discard_ConflictsOnProcessing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "discard@example.com")
	path := writeOnDiskImage(t, root, u.ID, "processing.jpg")
	jobID := insertScanJob(t, db, u.ID, scanJobStatusProcessing, path)

	rec := callJSON(t, http.MethodPost, "/api/pokemon/scans/"+strconv.FormatInt(jobID, 10)+"/resolve",
		map[string]any{"action": "discard"}, u,
		map[string]string{"id": strconv.FormatInt(jobID, 10)}, ResolveScanHandler(db))
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 for processing scan, got %d: %s", rec.Code, rec.Body.String())
	}
}

// setScanCapPref sets the per-user pokemon_scan_daily_cap override directly
// in the DB so the cap-related tests do not need to round-trip through the
// settings handler.
func setScanCapPref(t *testing.T, db *sql.DB, userID int64, value int) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO user_preferences (user_id, key, value)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value
	`, userID, "pokemon_scan_daily_cap", strconv.Itoa(value)); err != nil {
		t.Fatalf("set scan cap pref: %v", err)
	}
}

// scanJobCount returns how many pokemon_scan_jobs rows exist for the user —
// used by dedupe tests to confirm a duplicate upload did not insert a new row.
func scanJobCount(t *testing.T, db *sql.DB, userID int64) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pokemon_scan_jobs WHERE user_id = ?`, userID).Scan(&n); err != nil {
		t.Fatalf("count scan jobs: %v", err)
	}
	return n
}

// seedScanJobsToday backfills a count of rows dated to "right now" so the
// cap tests can put a user at the threshold without uploading hundreds of
// times through the handler.
func seedScanJobsToday(t *testing.T, db *sql.DB, userID int64, count int) {
	t.Helper()
	now := time.Now().UTC()
	for i := 0; i < count; i++ {
		insertScanJob(t, db, userID, scanJobStatusAdded, "", withCreatedAt(now))
	}
}

func TestQueueScan_AtCapReturns429(t *testing.T) {
	t.Setenv("UPLOAD_ROOT", t.TempDir())
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "cap@example.com")

	// Drop the cap to a tiny value so we don't have to seed 600 rows.
	setScanCapPref(t, db, u.ID, 3)
	seedScanJobsToday(t, db, u.ID, 3)

	payload := append([]byte{}, jpegMagic...)
	payload = append(payload, []byte("over-cap")...)

	req := asUser(buildQueueRequest(t, payload, "card.jpg"), u)
	rec := httptest.NewRecorder()
	QueueScanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Error string `json:"error"`
		Cap   int    `json:"cap"`
		Used  int    `json:"used"`
	}](t, rec)
	if body.Error != "daily scan cap reached" {
		t.Errorf("unexpected error message: %q", body.Error)
	}
	if body.Cap != 3 || body.Used != 3 {
		t.Errorf("expected cap=3 used=3, got cap=%d used=%d", body.Cap, body.Used)
	}
	if got := scanJobCount(t, db, u.ID); got != 3 {
		t.Errorf("expected no new row on 429, got count=%d", got)
	}
}

func TestQueueScan_Dedupe_Within10s(t *testing.T) {
	t.Setenv("UPLOAD_ROOT", t.TempDir())
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "dedupe@example.com")

	payload := append([]byte{}, jpegMagic...)
	payload = append(payload, []byte("identical-bytes")...)

	rec1 := httptest.NewRecorder()
	QueueScanHandler(db).ServeHTTP(rec1, asUser(buildQueueRequest(t, payload, "card.jpg"), u))
	if rec1.Code != http.StatusAccepted {
		t.Fatalf("first upload: expected 202, got %d: %s", rec1.Code, rec1.Body.String())
	}
	first := decode[struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}](t, rec1)
	if rec1.Header().Get("X-Pokemon-Scan-Dedupe") == "true" {
		t.Errorf("first upload should not be flagged as a dedupe")
	}

	rec2 := httptest.NewRecorder()
	QueueScanHandler(db).ServeHTTP(rec2, asUser(buildQueueRequest(t, payload, "card.jpg"), u))
	if rec2.Code != http.StatusAccepted {
		t.Fatalf("second upload: expected 202, got %d: %s", rec2.Code, rec2.Body.String())
	}
	second := decode[struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}](t, rec2)
	if rec2.Header().Get("X-Pokemon-Scan-Dedupe") != "true" {
		t.Errorf("second upload missing X-Pokemon-Scan-Dedupe header")
	}
	if second.ID != first.ID {
		t.Errorf("expected dedupe to return original job id=%d, got %d", first.ID, second.ID)
	}
	if got := scanJobCount(t, db, u.ID); got != 1 {
		t.Errorf("expected exactly 1 row after dedupe, got %d", got)
	}
}

func TestQueueScan_Dedupe_AfterWindowExpires(t *testing.T) {
	t.Setenv("UPLOAD_ROOT", t.TempDir())
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "stale@example.com")

	// Compute the SHA-256 the handler will derive from the upload bytes,
	// then seed a row with that same hash dated *outside* the dedupe
	// window. The handler must treat the incoming upload as fresh (no
	// dedupe header) and insert a second row alongside the stale one.
	payload := append([]byte{}, jpegMagic...)
	payload = append(payload, []byte("identical-but-stale")...)
	sum := sha256.Sum256(payload)
	imageHash := hex.EncodeToString(sum[:])

	if _, err := db.Exec(`
		INSERT INTO pokemon_scan_jobs (user_id, status, image_path_enc, image_hash, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, u.ID, scanJobStatusQueued, "", imageHash, time.Now().UTC().Add(-scanDedupeWindow-time.Second)); err != nil {
		t.Fatalf("seed old row: %v", err)
	}

	rec := httptest.NewRecorder()
	QueueScanHandler(db).ServeHTTP(rec, asUser(buildQueueRequest(t, payload, "card.jpg"), u))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Pokemon-Scan-Dedupe") == "true" {
		t.Errorf("expected fresh insert after window expiry, got dedupe flag")
	}
	if got := scanJobCount(t, db, u.ID); got != 2 {
		t.Errorf("expected 2 rows (stale + new), got %d", got)
	}
}

// TestFindRecentDuplicateScan_OutsideWindow exercises the helper directly:
// a row created beyond scanDedupeWindow ago must not match.
func TestFindRecentDuplicateScan_OutsideWindow(t *testing.T) {
	t.Setenv("UPLOAD_ROOT", t.TempDir())
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "stale@example.com")

	imageHash := "feedfacefeedface"
	if _, err := db.Exec(`
		INSERT INTO pokemon_scan_jobs (user_id, status, image_path_enc, image_hash, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, u.ID, scanJobStatusQueued, "", imageHash, time.Now().UTC().Add(-scanDedupeWindow-time.Second)); err != nil {
		t.Fatalf("seed old row: %v", err)
	}

	id, found, err := findRecentDuplicateScan(context.Background(), db, u.ID, imageHash)
	if err != nil {
		t.Fatalf("findRecentDuplicateScan: %v", err)
	}
	if found {
		t.Errorf("expected no dedupe hit outside window, got id=%d", id)
	}
}

func TestQueueScan_PreferenceOverride_RaisesCapForUser(t *testing.T) {
	t.Setenv("UPLOAD_ROOT", t.TempDir())
	db := setupTestDB(t)
	uA := seedUser(t, db, 1, "raised@example.com")
	uB := seedUser(t, db, 2, "default@example.com")

	// uA gets a generous override; uB gets a much lower one. This proves
	// the preference is read per-user rather than globally — same
	// queue-count for both users yields different cap outcomes. Both are
	// kept small so the test does not have to seed hundreds of rows.
	setScanCapPref(t, db, uA.ID, 10)
	setScanCapPref(t, db, uB.ID, 2)

	// Push both users right up to their respective caps.
	seedScanJobsToday(t, db, uA.ID, 2)
	seedScanJobsToday(t, db, uB.ID, 2)

	payload := append([]byte{}, jpegMagic...)
	payload = append(payload, []byte("a-bytes")...)

	// User A: still well under 10, should succeed.
	recA := httptest.NewRecorder()
	QueueScanHandler(db).ServeHTTP(recA, asUser(buildQueueRequest(t, payload, "a.jpg"), uA))
	if recA.Code != http.StatusAccepted {
		t.Errorf("expected user A to be under raised cap, got %d: %s", recA.Code, recA.Body.String())
	}

	// User B: at cap, should be rejected.
	payloadB := append([]byte{}, jpegMagic...)
	payloadB = append(payloadB, []byte("b-bytes")...)
	recB := httptest.NewRecorder()
	QueueScanHandler(db).ServeHTTP(recB, asUser(buildQueueRequest(t, payloadB, "b.jpg"), uB))
	if recB.Code != http.StatusTooManyRequests {
		t.Errorf("expected user B to be at default-low cap (429), got %d: %s", recB.Code, recB.Body.String())
	}
}

func TestListScans_IncludesTodayCounts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "today@example.com")
	setScanCapPref(t, db, u.ID, 25)
	seedScanJobsToday(t, db, u.ID, 4)

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/scans", nil), u)
	rec := httptest.NewRecorder()
	ListScansHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Scans []ScanJobDTO   `json:"scans"`
		Today map[string]int `json:"today"`
	}](t, rec)
	if body.Today == nil {
		t.Fatalf("expected today metadata in response")
	}
	if body.Today["cap"] != 25 {
		t.Errorf("expected today.cap=25 (from pref), got %d", body.Today["cap"])
	}
	if body.Today["used"] != 4 {
		t.Errorf("expected today.used=4, got %d", body.Today["used"])
	}
}

func TestListScans_TodayCounts_DefaultCap(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "default@example.com")

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/scans", nil), u)
	rec := httptest.NewRecorder()
	ListScansHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := decode[struct {
		Today map[string]int `json:"today"`
	}](t, rec)
	if body.Today["cap"] != ScanDailyCap {
		t.Errorf("expected default cap=%d, got %d", ScanDailyCap, body.Today["cap"])
	}
	if body.Today["used"] != 0 {
		t.Errorf("expected used=0 for new user, got %d", body.Today["used"])
	}
}

func TestGetUserScanDailyCap_FallsBackOnInvalidPref(t *testing.T) {
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "bad@example.com")

	// A non-integer value must collapse to the default rather than 0/error.
	if _, err := db.Exec(`
		INSERT INTO user_preferences (user_id, key, value) VALUES (?, ?, ?)
	`, u.ID, "pokemon_scan_daily_cap", "nonsense"); err != nil {
		t.Fatalf("seed bad pref: %v", err)
	}
	got, err := getUserScanDailyCap(context.Background(), db, u.ID)
	if err != nil {
		t.Fatalf("getUserScanDailyCap: %v", err)
	}
	if got != ScanDailyCap {
		t.Errorf("expected fallback=%d, got %d", ScanDailyCap, got)
	}
}

func TestScanCounts_AggregatesUnresolvedAndToday(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "counts@example.com")
	setScanCapPref(t, db, u.ID, 42)

	// 1 matched + 1 no_match + 1 failed → unresolved=3.
	// 1 queued (counts toward today_used but NOT unresolved).
	// 1 added (terminal → not unresolved).
	insertScanJob(t, db, u.ID, scanJobStatusMatched, "", withMatchedCard("sv1-25", 0.9))
	insertScanJob(t, db, u.ID, scanJobStatusNoMatch, "")
	insertScanJob(t, db, u.ID, scanJobStatusFailed, "", withError("boom"))
	insertScanJob(t, db, u.ID, scanJobStatusQueued, "")
	insertScanJob(t, db, u.ID, scanJobStatusAdded, "")

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/scans/counts", nil), u)
	rec := httptest.NewRecorder()
	ScanCountsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[map[string]int](t, rec)
	if body["unresolved"] != 3 {
		t.Errorf("expected unresolved=3, got %d", body["unresolved"])
	}
	if body["today_used"] != 5 {
		t.Errorf("expected today_used=5 (all 5 rows created today), got %d", body["today_used"])
	}
	if body["today_cap"] != 42 {
		t.Errorf("expected today_cap=42 (from pref), got %d", body["today_cap"])
	}
}

// TestResolveScanAdd_WithCardOverride exercises the "Wrong match?" reassign
// flow: the scan was auto-matched to card A but the user picks card B during
// review. The collection upsert must land on B, the job row must be rewritten
// so matched_card_id reflects B, and the image must be deleted as on a normal
// add.
func TestResolveScanAdd_WithCardOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "override@example.com")
	path := writeOnDiskImage(t, root, u.ID, "matched.jpg")
	// Auto-match landed on sv1-25, but the actual card was sv1-100.
	jobID := insertScanJob(t, db, u.ID, scanJobStatusMatched, path, withMatchedCard("sv1-25", 0.55))
	overrideVid := variantID(t, db, "sv1-100", "normal")

	rec := callJSON(t, http.MethodPost, "/api/pokemon/scans/"+strconv.FormatInt(jobID, 10)+"/resolve",
		map[string]any{"action": "add", "card_id": "sv1-100", "variant_id": overrideVid, "quantity": 1, "condition": "near_mint"},
		u, map[string]string{"id": strconv.FormatInt(jobID, 10)}, ResolveScanHandler(db))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Collection row should be on the override card, not the original.
	var qty int
	if err := db.QueryRow(
		`SELECT quantity FROM pokemon_collections WHERE user_id = ? AND card_id = ? AND variant_id = ?`,
		u.ID, "sv1-100", overrideVid,
	).Scan(&qty); err != nil {
		t.Fatalf("expected collection row on override card sv1-100: %v", err)
	}
	if qty != 1 {
		t.Errorf("expected quantity=1 on override card, got %d", qty)
	}

	// And no row should exist for the original (wrong) match.
	var leak int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pokemon_collections WHERE user_id = ? AND card_id = ?`,
		u.ID, "sv1-25",
	).Scan(&leak); err != nil {
		t.Fatalf("count leak: %v", err)
	}
	if leak != 0 {
		t.Errorf("expected no collection row on auto-matched card, got %d", leak)
	}

	// Job row should be 'added' with matched_card_id rewritten to the override.
	status, pathEnc, _, resolvedAt, matchedCard, matchedVariant := readScanJobByID(t, db, jobID)
	if status != scanJobStatusAdded {
		t.Errorf("expected status=added, got %q", status)
	}
	if !matchedCard.Valid || matchedCard.String != "sv1-100" {
		t.Errorf("expected matched_card_id rewritten to override sv1-100, got %+v", matchedCard)
	}
	if !matchedVariant.Valid || matchedVariant.Int64 != overrideVid {
		t.Errorf("expected matched_variant_id=%d, got %+v", overrideVid, matchedVariant)
	}
	if !resolvedAt.Valid {
		t.Errorf("expected resolved_at to be set")
	}
	if pathEnc != "" {
		t.Errorf("expected image_path_enc to be cleared, got %q", pathEnc)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected image file to be deleted: stat err=%v", err)
	}
}

// TestResolveScanAdd_OverrideUnknownCard_404 covers the case where the client
// supplies an override card_id that does not exist in the catalogue.
func TestResolveScanAdd_OverrideUnknownCard_404(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "override404@example.com")
	path := writeOnDiskImage(t, root, u.ID, "matched.jpg")
	jobID := insertScanJob(t, db, u.ID, scanJobStatusMatched, path, withMatchedCard("sv1-25", 0.9))
	vid := variantID(t, db, "sv1-25", "normal")

	rec := callJSON(t, http.MethodPost, "/api/pokemon/scans/"+strconv.FormatInt(jobID, 10)+"/resolve",
		map[string]any{"action": "add", "card_id": "definitely-not-a-card", "variant_id": vid, "quantity": 1},
		u, map[string]string{"id": strconv.FormatInt(jobID, 10)}, ResolveScanHandler(db))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown override card, got %d: %s", rec.Code, rec.Body.String())
	}

	// Job row should be untouched — still matched, image still on disk.
	status, _, _, _, _, _ := readScanJobByID(t, db, jobID)
	if status != scanJobStatusMatched {
		t.Errorf("expected status to stay matched after 404, got %q", status)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected image preserved on 404, got %v", err)
	}
}

// TestResolveScanAdd_OverrideVariantMismatch_400 covers the case where the
// supplied variant_id belongs to a different card than the override card.
func TestResolveScanAdd_OverrideVariantMismatch_400(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "overrideMismatch@example.com")
	path := writeOnDiskImage(t, root, u.ID, "matched.jpg")
	jobID := insertScanJob(t, db, u.ID, scanJobStatusMatched, path, withMatchedCard("sv1-25", 0.9))
	// Variant belongs to sv1-25 (auto-match), but we override to sv1-100.
	wrongVid := variantID(t, db, "sv1-25", "normal")

	rec := callJSON(t, http.MethodPost, "/api/pokemon/scans/"+strconv.FormatInt(jobID, 10)+"/resolve",
		map[string]any{"action": "add", "card_id": "sv1-100", "variant_id": wrongVid, "quantity": 1},
		u, map[string]string{"id": strconv.FormatInt(jobID, 10)}, ResolveScanHandler(db))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for variant mismatch with override, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestScanCounts_OtherUsersDoNotLeak(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	uA := seedUser(t, db, 1, "a@example.com")
	uB := seedUser(t, db, 2, "b@example.com")

	insertScanJob(t, db, uB.ID, scanJobStatusMatched, "", withMatchedCard("sv1-25", 0.9))
	insertScanJob(t, db, uB.ID, scanJobStatusFailed, "", withError("boom"))

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/scans/counts", nil), uA)
	rec := httptest.NewRecorder()
	ScanCountsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[map[string]int](t, rec)
	if body["unresolved"] != 0 {
		t.Errorf("expected unresolved=0 for user A (no jobs of their own), got %d", body["unresolved"])
	}
	if body["today_cap"] != ScanDailyCap {
		t.Errorf("expected today_cap=%d (default), got %d", ScanDailyCap, body["today_cap"])
	}
}

// buildPageScanRequest builds a multipart upload for /api/pokemon/scans/page
// with one image part per supplied payload plus a JSON cells field. cells must
// have the same length as payloads — the tests deliberately feed identical
// lengths because the matching check is exercised by the dedicated mismatch
// test below.
func buildPageScanRequest(t *testing.T, payloads [][]byte, cells string) *http.Request {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	for i, p := range payloads {
		part, err := w.CreateFormFile("images", "card-"+strconv.Itoa(i)+".jpg")
		if err != nil {
			t.Fatalf("create form file %d: %v", i, err)
		}
		if _, err := part.Write(p); err != nil {
			t.Fatalf("write form payload %d: %v", i, err)
		}
	}
	if cells != "" {
		if err := w.WriteField("cells", cells); err != nil {
			t.Fatalf("write cells field: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/pokemon/scans/page", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// jpegPayload returns N distinct JPEG-magic-prefixed byte slices so each card
// part has its own SHA-256 hash. Used by the page-upload tests to confirm
// children are stored independently rather than collapsed via dedupe.
func jpegPayloads(t *testing.T, n int) [][]byte {
	t.Helper()
	out := make([][]byte, n)
	for i := 0; i < n; i++ {
		out[i] = append([]byte{}, jpegMagic...)
		out[i] = append(out[i], []byte("page-card-"+strconv.Itoa(i))...)
	}
	return out
}

// TestPageScan_NPartsProduceOneParentAndNChildren is the happy-path: 9 image
// parts plus a 9-element cells array must land 1 row in pokemon_scan_pages
// and 9 children in pokemon_scan_jobs all linked via page_id, plus 9 image
// files written to disk.
func TestPageScan_NPartsProduceOneParentAndNChildren(t *testing.T) {
	t.Setenv("UPLOAD_ROOT", t.TempDir())
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "page@example.com")

	const n = 9
	payloads := jpegPayloads(t, n)
	cells := `[{"row":0,"col":0},{"row":0,"col":1},{"row":0,"col":2},{"row":1,"col":0},{"row":1,"col":1},{"row":1,"col":2},{"row":2,"col":0},{"row":2,"col":1},{"row":2,"col":2}]`

	req := asUser(buildPageScanRequest(t, payloads, cells), u)
	rec := httptest.NewRecorder()
	PageScanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		PageID int64   `json:"page_id"`
		JobIDs []int64 `json:"job_ids"`
		Count  int     `json:"count"`
	}](t, rec)
	if body.PageID <= 0 {
		t.Fatalf("expected page_id > 0, got %d", body.PageID)
	}
	if len(body.JobIDs) != n || body.Count != n {
		t.Fatalf("expected %d job ids and count=%d, got ids=%d count=%d", n, n, len(body.JobIDs), body.Count)
	}

	var pageCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pokemon_scan_pages WHERE user_id = ?`, u.ID).Scan(&pageCount); err != nil {
		t.Fatalf("count pages: %v", err)
	}
	if pageCount != 1 {
		t.Errorf("expected exactly 1 pokemon_scan_pages row, got %d", pageCount)
	}
	var expectedCount int
	if err := db.QueryRow(`SELECT expected_count FROM pokemon_scan_pages WHERE id = ?`, body.PageID).Scan(&expectedCount); err != nil {
		t.Fatalf("read expected_count: %v", err)
	}
	if expectedCount != n {
		t.Errorf("expected expected_count=%d, got %d", n, expectedCount)
	}

	var childCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pokemon_scan_jobs WHERE user_id = ? AND page_id = ?`, u.ID, body.PageID).Scan(&childCount); err != nil {
		t.Fatalf("count children: %v", err)
	}
	if childCount != n {
		t.Errorf("expected %d children linked to page_id=%d, got %d", n, body.PageID, childCount)
	}

	// Every child must have its own decryptable image on disk so the worker
	// can pick it up via the same code path as a single-card upload.
	rows, err := db.Query(`SELECT image_path_enc FROM pokemon_scan_jobs WHERE page_id = ? ORDER BY id`, body.PageID)
	if err != nil {
		t.Fatalf("query child paths: %v", err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var enc string
		if err := rows.Scan(&enc); err != nil {
			t.Fatalf("scan path: %v", err)
		}
		path, err := encryption.DecryptField(enc)
		if err != nil || path == "" {
			t.Fatalf("decrypt child path: err=%v path=%q", err, path)
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected child image at %s: %v", path, err)
		}
		seen++
	}
	if seen != n {
		t.Errorf("expected %d child paths, scanned %d", n, seen)
	}
}

// TestPageScan_CostCapReturns429NoImageWritten verifies the strict order of
// operations: when the user does not have N free scans left today the handler
// returns 429 and leaves zero state — no parent row, no children, and no
// files on the per-user scan directory.
func TestPageScan_CostCapReturns429NoImageWritten(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "cap-page@example.com")

	// Cap=5, already used=3 → remaining=2 < n=4. The 4-image upload must
	// be refused without writing anything.
	setScanCapPref(t, db, u.ID, 5)
	seedScanJobsToday(t, db, u.ID, 3)
	usedBefore := scanJobCount(t, db, u.ID)

	const n = 4
	payloads := jpegPayloads(t, n)
	cells := `[{"row":0,"col":0},{"row":0,"col":1},{"row":1,"col":0},{"row":1,"col":1}]`

	req := asUser(buildPageScanRequest(t, payloads, cells), u)
	rec := httptest.NewRecorder()
	PageScanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Error     string `json:"error"`
		Cap       int    `json:"cap"`
		Used      int    `json:"used"`
		Requested int    `json:"requested"`
	}](t, rec)
	if body.Error != "daily scan cap reached" {
		t.Errorf("unexpected error message: %q", body.Error)
	}
	if body.Cap != 5 || body.Used != 3 || body.Requested != n {
		t.Errorf("expected cap=5 used=3 requested=%d, got cap=%d used=%d requested=%d",
			n, body.Cap, body.Used, body.Requested)
	}

	var pageRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pokemon_scan_pages WHERE user_id = ?`, u.ID).Scan(&pageRows); err != nil {
		t.Fatalf("count pages: %v", err)
	}
	if pageRows != 0 {
		t.Errorf("expected zero pokemon_scan_pages rows on 429, got %d", pageRows)
	}
	if got := scanJobCount(t, db, u.ID); got != usedBefore {
		t.Errorf("expected child job count to stay at %d on 429, got %d", usedBefore, got)
	}

	userScanDir := filepath.Join(root, "pokemon-scans", strconv.FormatInt(u.ID, 10))
	if entries, err := os.ReadDir(userScanDir); err == nil {
		// Directory may not exist at all — that's fine. If it does,
		// it must contain no files because the 429 fired before any write.
		if len(entries) != 0 {
			t.Errorf("expected no scan files written on 429, found %d entries in %s", len(entries), userScanDir)
		}
	}
}

// TestPageScan_CellsLengthMustMatchImageCount guards the contract that the
// caller cannot smuggle in extra or missing crops vs the cells JSON.
func TestPageScan_CellsLengthMustMatchImageCount(t *testing.T) {
	t.Setenv("UPLOAD_ROOT", t.TempDir())
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "mismatch@example.com")

	payloads := jpegPayloads(t, 3)
	// 2 cells, 3 images — must reject without writing rows or files.
	cells := `[{"row":0,"col":0},{"row":0,"col":1}]`

	req := asUser(buildPageScanRequest(t, payloads, cells), u)
	rec := httptest.NewRecorder()
	PageScanHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on cells/images mismatch, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := scanJobCount(t, db, u.ID); got != 0 {
		t.Errorf("expected no rows on 400, got %d", got)
	}
}

// TestPageScan_RejectsBadMIME verifies a non-image part rejects the whole
// batch — no children inserted and no files written.
func TestPageScan_RejectsBadMIME(t *testing.T) {
	t.Setenv("UPLOAD_ROOT", t.TempDir())
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "badmime@example.com")

	good := append([]byte{}, jpegMagic...)
	good = append(good, []byte("ok")...)
	bad := []byte("definitely not an image")
	cells := `[{"row":0,"col":0},{"row":0,"col":1}]`

	req := asUser(buildPageScanRequest(t, [][]byte{good, bad}, cells), u)
	rec := httptest.NewRecorder()
	PageScanHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad MIME part, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := scanJobCount(t, db, u.ID); got != 0 {
		t.Errorf("expected no rows on bad MIME, got %d", got)
	}
	var pageRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pokemon_scan_pages WHERE user_id = ?`, u.ID).Scan(&pageRows); err != nil {
		t.Fatalf("count pages: %v", err)
	}
	if pageRows != 0 {
		t.Errorf("expected no pokemon_scan_pages on bad MIME, got %d", pageRows)
	}
}

// insertScanPage seeds a pokemon_scan_pages parent row directly so list /
// delete tests can build a known-shape page without going through the
// multipart upload handler.
func insertScanPage(t *testing.T, db *sql.DB, userID int64, expected int, createdAt time.Time) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO pokemon_scan_pages (user_id, page_image_path_enc, expected_count, matched_count, created_at)
		VALUES (?, '', ?, 0, ?)
	`, userID, expected, createdAt)
	if err != nil {
		t.Fatalf("insert scan page: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("scan page last id: %v", err)
	}
	return id
}

// linkScanJobToPage stamps page_id on an existing scan job. Tests use this
// to attach previously-inserted children to a freshly-seeded parent.
func linkScanJobToPage(t *testing.T, db *sql.DB, jobID, pageID int64) {
	t.Helper()
	if _, err := db.Exec(`UPDATE pokemon_scan_jobs SET page_id = ? WHERE id = ?`, pageID, jobID); err != nil {
		t.Fatalf("link scan job to page: %v", err)
	}
}

// TestListScans_PageReturnsAllChildrenRegardlessOfStatus is the core grid
// contract: when the status filter matches any child of a page, the list
// response must include the parent and ALL of its children (even those whose
// individual status falls outside the filter) so the frontend can render the
// full captured layout in one block.
func TestListScans_PageReturnsAllChildrenRegardlessOfStatus(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "page-list@example.com")

	now := time.Now().UTC()
	pageID := insertScanPage(t, db, u.ID, 4, now)
	// Four children spanning the full lifecycle: only one matches the
	// "needs review" filter (matched), but all four must come back.
	p1 := writeOnDiskImage(t, root, u.ID, "queued.jpg")
	p2 := writeOnDiskImage(t, root, u.ID, "match.jpg")
	p3 := writeOnDiskImage(t, root, u.ID, "no-match.jpg")
	p4 := writeOnDiskImage(t, root, u.ID, "failed.jpg")
	c1 := insertScanJob(t, db, u.ID, scanJobStatusQueued, p1, withCreatedAt(now))
	c2 := insertScanJob(t, db, u.ID, scanJobStatusMatched, p2,
		withCreatedAt(now), withMatchedCard("sv1-25", 0.92))
	c3 := insertScanJob(t, db, u.ID, scanJobStatusNoMatch, p3,
		withCreatedAt(now), withError("low confidence"))
	c4 := insertScanJob(t, db, u.ID, scanJobStatusFailed, p4,
		withCreatedAt(now), withError("boom"))
	linkScanJobToPage(t, db, c1, pageID)
	linkScanJobToPage(t, db, c2, pageID)
	linkScanJobToPage(t, db, c3, pageID)
	linkScanJobToPage(t, db, c4, pageID)

	req := asUser(httptest.NewRequest(http.MethodGet,
		"/api/pokemon/scans?status=matched,no_match,failed", nil), u)
	rec := httptest.NewRecorder()
	ListScansHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Scans []ScanJobDTO  `json:"scans"`
		Pages []ScanPageDTO `json:"pages"`
	}](t, rec)
	if len(body.Scans) != 0 {
		t.Errorf("expected zero standalone scans, got %d", len(body.Scans))
	}
	if len(body.Pages) != 1 {
		t.Fatalf("expected one page, got %d", len(body.Pages))
	}
	page := body.Pages[0]
	if page.ID != pageID {
		t.Errorf("expected page id=%d, got %d", pageID, page.ID)
	}
	if page.ExpectedCount != 4 {
		t.Errorf("expected expected_count=4, got %d", page.ExpectedCount)
	}
	if len(page.Children) != 4 {
		t.Fatalf("expected all 4 children, got %d", len(page.Children))
	}
	gotStatuses := map[string]bool{}
	for _, child := range page.Children {
		gotStatuses[child.Status] = true
	}
	for _, want := range []string{scanJobStatusQueued, scanJobStatusMatched, scanJobStatusNoMatch, scanJobStatusFailed} {
		if !gotStatuses[want] {
			t.Errorf("expected child with status %q in grid, got statuses %v", want, gotStatuses)
		}
	}
	// The matched child must have its card hydrated so the grid can render
	// the card name without an extra round trip.
	var matchedChild *ScanJobDTO
	for i := range page.Children {
		if page.Children[i].Status == scanJobStatusMatched {
			matchedChild = &page.Children[i]
			break
		}
	}
	if matchedChild == nil || matchedChild.MatchedCard == nil || matchedChild.MatchedCard.ID != "sv1-25" {
		t.Errorf("expected matched child to carry hydrated card sv1-25, got %+v", matchedChild)
	}
}

// TestListScans_StandaloneAndPageInSameResponse verifies that page-grouped
// scans and standalone scans coexist in the response without collisions:
// standalone rows stay in `scans`, pages go in `pages`, and a single matched
// card is hydrated correctly in both buckets.
func TestListScans_StandaloneAndPageInSameResponse(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "mixed@example.com")

	now := time.Now().UTC()
	standalonePath := writeOnDiskImage(t, root, u.ID, "standalone.jpg")
	insertScanJob(t, db, u.ID, scanJobStatusMatched, standalonePath,
		withCreatedAt(now.Add(-1*time.Minute)), withMatchedCard("sv1-25", 0.9))

	pageID := insertScanPage(t, db, u.ID, 2, now)
	pp1 := writeOnDiskImage(t, root, u.ID, "page-child.jpg")
	pc := insertScanJob(t, db, u.ID, scanJobStatusMatched, pp1,
		withCreatedAt(now), withMatchedCard("sv1-25", 0.9))
	linkScanJobToPage(t, db, pc, pageID)

	req := asUser(httptest.NewRequest(http.MethodGet,
		"/api/pokemon/scans?status=matched", nil), u)
	rec := httptest.NewRecorder()
	ListScansHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Scans []ScanJobDTO  `json:"scans"`
		Pages []ScanPageDTO `json:"pages"`
	}](t, rec)
	if len(body.Scans) != 1 {
		t.Fatalf("expected one standalone scan, got %d", len(body.Scans))
	}
	if body.Scans[0].MatchedCard == nil || body.Scans[0].MatchedCard.ID != "sv1-25" {
		t.Errorf("expected standalone matched card hydrated, got %+v", body.Scans[0].MatchedCard)
	}
	if len(body.Pages) != 1 {
		t.Fatalf("expected one page, got %d", len(body.Pages))
	}
	if len(body.Pages[0].Children) != 1 {
		t.Errorf("expected one child in page, got %d", len(body.Pages[0].Children))
	}
}

// TestDeleteScanPage_DiscardsChildrenAndRemovesParent verifies the page-level
// discard: every non-terminal child is soft-discarded (status='discarded',
// image_path cleared), the parent row is removed, and the on-disk files are
// reaped.
func TestDeleteScanPage_DiscardsChildrenAndRemovesParent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "delete-page@example.com")

	now := time.Now().UTC()
	pageID := insertScanPage(t, db, u.ID, 3, now)
	p1 := writeOnDiskImage(t, root, u.ID, "del-1.jpg")
	p2 := writeOnDiskImage(t, root, u.ID, "del-2.jpg")
	p3 := writeOnDiskImage(t, root, u.ID, "del-3.jpg")
	c1 := insertScanJob(t, db, u.ID, scanJobStatusMatched, p1,
		withCreatedAt(now), withMatchedCard("sv1-25", 0.9))
	c2 := insertScanJob(t, db, u.ID, scanJobStatusNoMatch, p2, withCreatedAt(now))
	c3 := insertScanJob(t, db, u.ID, scanJobStatusQueued, p3, withCreatedAt(now))
	linkScanJobToPage(t, db, c1, pageID)
	linkScanJobToPage(t, db, c2, pageID)
	linkScanJobToPage(t, db, c3, pageID)

	req := asUser(httptest.NewRequest(http.MethodDelete,
		"/api/pokemon/scans/pages/"+strconv.FormatInt(pageID, 10), nil), u)
	req = asChi(req, map[string]string{"id": strconv.FormatInt(pageID, 10)})
	rec := httptest.NewRecorder()
	DeleteScanPageHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	var pageRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pokemon_scan_pages WHERE id = ?`, pageID).Scan(&pageRows); err != nil {
		t.Fatalf("count pages: %v", err)
	}
	if pageRows != 0 {
		t.Errorf("expected parent row deleted, found %d", pageRows)
	}

	for _, cID := range []int64{c1, c2, c3} {
		var status, pathEnc string
		if err := db.QueryRow(`SELECT status, image_path_enc FROM pokemon_scan_jobs WHERE id = ?`, cID).Scan(&status, &pathEnc); err != nil {
			t.Fatalf("read child %d: %v", cID, err)
		}
		if status != scanJobStatusDiscarded {
			t.Errorf("child %d expected status=discarded, got %q", cID, status)
		}
		if pathEnc != "" {
			t.Errorf("child %d expected cleared image_path_enc, got non-empty", cID)
		}
	}
	for _, p := range []string{p1, p2, p3} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected on-disk image %s removed, got err=%v", p, err)
		}
	}
}

// TestDeleteScanPage_PreservesAddedChildren ensures that children that the
// user has already added to their collection are left alone: page-discard is
// a "throw away the rest" action, not a retroactive removal of accepted cards.
func TestDeleteScanPage_PreservesAddedChildren(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "delete-keep@example.com")

	now := time.Now().UTC()
	pageID := insertScanPage(t, db, u.ID, 2, now)
	pAdded := writeOnDiskImage(t, root, u.ID, "added.jpg")
	pQueued := writeOnDiskImage(t, root, u.ID, "queued.jpg")
	addedID := insertScanJob(t, db, u.ID, scanJobStatusAdded, pAdded,
		withCreatedAt(now), withMatchedCard("sv1-25", 0.9))
	queuedID := insertScanJob(t, db, u.ID, scanJobStatusQueued, pQueued, withCreatedAt(now))
	linkScanJobToPage(t, db, addedID, pageID)
	linkScanJobToPage(t, db, queuedID, pageID)

	req := asUser(httptest.NewRequest(http.MethodDelete,
		"/api/pokemon/scans/pages/"+strconv.FormatInt(pageID, 10), nil), u)
	req = asChi(req, map[string]string{"id": strconv.FormatInt(pageID, 10)})
	rec := httptest.NewRecorder()
	DeleteScanPageHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	var addedStatus string
	if err := db.QueryRow(`SELECT status FROM pokemon_scan_jobs WHERE id = ?`, addedID).Scan(&addedStatus); err != nil {
		t.Fatalf("read added child: %v", err)
	}
	if addedStatus != scanJobStatusAdded {
		t.Errorf("expected already-added child preserved, got %q", addedStatus)
	}
	var queuedStatus string
	if err := db.QueryRow(`SELECT status FROM pokemon_scan_jobs WHERE id = ?`, queuedID).Scan(&queuedStatus); err != nil {
		t.Fatalf("read queued child: %v", err)
	}
	if queuedStatus != scanJobStatusDiscarded {
		t.Errorf("expected queued child discarded, got %q", queuedStatus)
	}
}

// TestDeleteScanPage_OtherUserReturns404 confirms cross-user isolation: a
// caller may not delete or even probe the existence of another user's page.
func TestDeleteScanPage_OtherUserReturns404(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	uA := seedUser(t, db, 1, "owner@example.com")
	uB := seedUser(t, db, 2, "stranger@example.com")

	pageID := insertScanPage(t, db, uA.ID, 1, time.Now().UTC())

	req := asUser(httptest.NewRequest(http.MethodDelete,
		"/api/pokemon/scans/pages/"+strconv.FormatInt(pageID, 10), nil), uB)
	req = asChi(req, map[string]string{"id": strconv.FormatInt(pageID, 10)})
	rec := httptest.NewRecorder()
	DeleteScanPageHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for other user's page, got %d", rec.Code)
	}
	// Parent row must survive an unauthorised delete attempt.
	var pageRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pokemon_scan_pages WHERE id = ?`, pageID).Scan(&pageRows); err != nil {
		t.Fatalf("count pages: %v", err)
	}
	if pageRows != 1 {
		t.Errorf("expected page still present, got %d rows", pageRows)
	}
}

// TestDeleteScanPage_RejectsWhileProcessing guards against the race where a
// scan worker finishes *after* the page-discard update and silently overwrites
// the 'discarded' status back to 'matched'/'no_match'/'failed'. The handler
// must return 409 Conflict when any child is still in 'processing' state.
func TestDeleteScanPage_RejectsWhileProcessing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPLOAD_ROOT", root)
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "processing-race@example.com")

	now := time.Now().UTC()
	pageID := insertScanPage(t, db, u.ID, 2, now)
	p1 := writeOnDiskImage(t, root, u.ID, "proc-1.jpg")
	p2 := writeOnDiskImage(t, root, u.ID, "proc-2.jpg")
	c1 := insertScanJob(t, db, u.ID, scanJobStatusProcessing, p1, withCreatedAt(now))
	c2 := insertScanJob(t, db, u.ID, scanJobStatusMatched, p2,
		withCreatedAt(now), withMatchedCard("sv1-25", 0.9))
	linkScanJobToPage(t, db, c1, pageID)
	linkScanJobToPage(t, db, c2, pageID)

	req := asUser(httptest.NewRequest(http.MethodDelete,
		"/api/pokemon/scans/pages/"+strconv.FormatInt(pageID, 10), nil), u)
	req = asChi(req, map[string]string{"id": strconv.FormatInt(pageID, 10)})
	rec := httptest.NewRecorder()
	DeleteScanPageHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 while a child is processing, got %d: %s", rec.Code, rec.Body.String())
	}
	// Parent must still exist and the child statuses must be unchanged.
	var pageRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pokemon_scan_pages WHERE id = ?`, pageID).Scan(&pageRows); err != nil {
		t.Fatalf("count pages: %v", err)
	}
	if pageRows != 1 {
		t.Errorf("expected page still present after rejected delete, got %d rows", pageRows)
	}
	var procStatus string
	if err := db.QueryRow(`SELECT status FROM pokemon_scan_jobs WHERE id = ?`, c1).Scan(&procStatus); err != nil {
		t.Fatalf("read processing child: %v", err)
	}
	if procStatus != scanJobStatusProcessing {
		t.Errorf("expected processing child status unchanged, got %q", procStatus)
	}
}
