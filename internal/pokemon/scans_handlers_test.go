package pokemon

import (
	"bytes"
	"context"
	"database/sql"
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
	res, err := db.Exec(`
		INSERT INTO pokemon_scan_jobs (user_id, status, image_path_enc, image_hash, matched_card_id,
			confidence, error_message, created_at, processed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, cfg.userID, cfg.status, cfg.pathEnc, cfg.imageHash, nullableString(cfg.matchedCard),
		nullableFloat(cfg.confidence), nullableString(cfg.errorMessage), cfg.createdAt,
		nullableTime(cfg.processedAt))
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
	userID       int64
	status       string
	pathEnc      string
	imageHash    string
	matchedCard  string
	confidence   float64
	errorMessage string
	createdAt    time.Time
	processedAt  time.Time
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
