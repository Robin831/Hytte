package familychat

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/go-chi/chi/v5"
)

// maxAttachmentBytes caps an attachment upload at 10 MiB. The MaxBytesReader
// gets a small slack budget for the multipart envelope itself.
const (
	maxAttachmentBytes        = 10 * 1024 * 1024
	multipartParseMemoryBytes = 10 * 1024 * 1024
)

// allowedAttachmentMimes is the strict allowlist of accepted upload types.
// Anything else is rejected with 400 before the file ever hits disk.
var allowedAttachmentMimes = map[string]struct{}{
	"image/jpeg":      {},
	"image/png":       {},
	"image/webp":      {},
	"image/heic":      {},
	"image/heif":      {},
	"application/pdf": {},
	"audio/mpeg":      {},
	"audio/mp4":       {},
}

// attachmentRoot returns the base directory under which Family Chat
// attachments live. Honours UPLOAD_ROOT for deploys that mount a dedicated
// volume; the production server's default is /var/lib/hytte. Mirrors the
// pattern used by pokemon/scan_worker.go so operators only need one env var.
func attachmentRoot() string {
	if root := os.Getenv("UPLOAD_ROOT"); root != "" {
		return filepath.Join(root, "familychat")
	}
	return "/var/lib/hytte/familychat"
}

// attachmentDir returns the per-conversation directory under attachmentRoot.
// Created with 0700 perms so attachments are unreadable by other OS accounts.
func attachmentDir(convID int64) (string, error) {
	dir := filepath.Join(attachmentRoot(), strconv.FormatInt(convID, 10))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create attachment dir %s: %w", dir, err)
	}
	return dir, nil
}

// attachmentUUID returns a 32-character hex random suitable for an
// unguessable filename. crypto/rand keeps the IDs from being brute-forced
// from an adjacent conversation.
func attachmentUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// resolveAttachmentPath joins conv dir + relative path and guards against
// traversal: the returned absolute path must live strictly under the
// conversation's attachment dir. Returns ("", false) when the relative
// path tries to escape (contains separator, ".." segment, or absolute path).
func resolveAttachmentPath(convID int64, rel string) (string, bool) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", false
	}
	// Reject anything that looks like a path — attachments are stored as a
	// flat UUID filename inside the conv directory.
	if strings.ContainsAny(rel, `/\`) || rel == "." || rel == ".." {
		return "", false
	}
	dir := filepath.Join(attachmentRoot(), strconv.FormatInt(convID, 10))
	full := filepath.Join(dir, rel)
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", false
	}
	if !strings.HasPrefix(absFull, absDir+string(os.PathSeparator)) && absFull != absDir {
		return "", false
	}
	return absFull, true
}

// AttachmentExists reports whether the attachment referenced by rel exists
// under the conversation's directory. Used by the POST /messages path to
// reject references to files the uploader never actually wrote.
func AttachmentExists(convID int64, rel string) bool {
	full, ok := resolveAttachmentPath(convID, rel)
	if !ok {
		return false
	}
	info, err := os.Stat(full)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// UploadAttachmentHandler accepts a multipart form upload, validates size +
// MIME against the allowlist, and writes the bytes to disk. Returns
// {upload_id, mime, size}; the client passes upload_id back as
// attachment_path in the subsequent POST /messages call.
func UploadAttachmentHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convID, ok := parseConvID(r)
		if !ok {
			notFound(w)
			return
		}
		isMember, err := IsMember(db, convID, user.ID)
		if err != nil {
			log.Printf("familychat: upload membership check conv=%d user=%d: %v", convID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to verify membership"})
			return
		}
		if !isMember {
			notFound(w)
			return
		}

		// Cap the body so a runaway upload cannot exhaust memory or disk.
		// +4096 leaves headroom for the multipart envelope itself.
		r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentBytes+4096)
		if err := r.ParseMultipartForm(multipartParseMemoryBytes); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "file too large"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form"})
			return
		}
		defer r.MultipartForm.RemoveAll()

		file, header, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no file provided"})
			return
		}
		defer file.Close()

		// Sniff the first 512 bytes to determine the real MIME type rather
		// than trust the client's Content-Type. Fall back to the client header
		// only if sniffing yields the generic octet-stream value, since some
		// types (HEIC, audio/mp4) are not in Go's built-in sniff table.
		sniffBuf := make([]byte, 512)
		n, _ := io.ReadFull(file, sniffBuf)
		sniffBuf = sniffBuf[:n]
		mimeType := http.DetectContentType(sniffBuf)
		// Strip charset suffix that DetectContentType sometimes appends.
		if idx := strings.Index(mimeType, ";"); idx >= 0 {
			mimeType = strings.TrimSpace(mimeType[:idx])
		}
		if mimeType == "application/octet-stream" {
			if ct := strings.TrimSpace(header.Header.Get("Content-Type")); ct != "" {
				if idx := strings.Index(ct, ";"); idx >= 0 {
					ct = strings.TrimSpace(ct[:idx])
				}
				mimeType = strings.ToLower(ct)
			}
		}
		if _, allowed := allowedAttachmentMimes[mimeType]; !allowed {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported file type"})
			return
		}

		uuid, err := attachmentUUID()
		if err != nil {
			log.Printf("familychat: upload generate uuid: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to allocate id"})
			return
		}
		dir, err := attachmentDir(convID)
		if err != nil {
			log.Printf("familychat: upload mkdir conv=%d: %v", convID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create storage dir"})
			return
		}
		dst := filepath.Join(dir, uuid)
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			log.Printf("familychat: upload open dst conv=%d: %v", convID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store file"})
			return
		}

		// Write the sniffed prefix back, then the remaining stream. The
		// total bytes written are bounded by maxAttachmentBytes — we used
		// MaxBytesReader above so the underlying file reader will error if
		// the caller tries to exceed that limit.
		total := int64(0)
		if len(sniffBuf) > 0 {
			wn, werr := out.Write(sniffBuf)
			total += int64(wn)
			if werr != nil {
				out.Close()
				_ = os.Remove(dst)
				log.Printf("familychat: upload write prefix conv=%d: %v", convID, werr)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store file"})
				return
			}
		}
		copied, copyErr := io.Copy(out, file)
		total += copied
		closeErr := out.Close()
		if copyErr != nil {
			_ = os.Remove(dst)
			var maxBytesErr *http.MaxBytesError
			if errors.As(copyErr, &maxBytesErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "file too large"})
				return
			}
			log.Printf("familychat: upload copy conv=%d: %v", convID, copyErr)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store file"})
			return
		}
		if closeErr != nil {
			_ = os.Remove(dst)
			log.Printf("familychat: upload close conv=%d: %v", convID, closeErr)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store file"})
			return
		}
		if total > maxAttachmentBytes {
			_ = os.Remove(dst)
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "file too large"})
			return
		}

		// Persist the server-determined MIME alongside the data file so that
		// POST /messages can retrieve it without trusting the client to
		// round-trip the correct type. Failure is non-fatal: the attachment is
		// already on disk and serving falls back to content sniffing.
		if err := os.WriteFile(dst+".mime", []byte(mimeType), 0600); err != nil {
			log.Printf("familychat: upload mime sidecar conv=%d: %v", convID, err)
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"upload_id": uuid,
			"mime":      mimeType,
			"size":      total,
		})
	}
}

// GetAttachmentHandler streams the attachment bytes for a single message.
// Membership-checked; non-members and unknown messages both get 404 so the
// existence of attachments in conversations the caller cannot read is not
// leaked.
func GetAttachmentHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		convID, ok := parseConvID(r)
		if !ok {
			notFound(w)
			return
		}
		msgID, err := strconv.ParseInt(chi.URLParam(r, "message_id"), 10, 64)
		if err != nil || msgID <= 0 {
			notFound(w)
			return
		}
		isMember, err := IsMember(db, convID, user.ID)
		if err != nil {
			log.Printf("familychat: attachment membership conv=%d user=%d: %v", convID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to verify membership"})
			return
		}
		if !isMember {
			notFound(w)
			return
		}

		var encPath, mime string
		err = db.QueryRow(
			`SELECT attachment_path, attachment_mime FROM family_chat_messages WHERE id = ? AND conversation_id = ?`,
			msgID, convID,
		).Scan(&encPath, &mime)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				notFound(w)
				return
			}
			log.Printf("familychat: attachment row conv=%d msg=%d: %v", convID, msgID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load attachment"})
			return
		}
		if encPath == "" {
			notFound(w)
			return
		}
		rel, err := encryption.DecryptField(encPath)
		if err != nil {
			log.Printf("familychat: attachment decrypt conv=%d msg=%d: %v", convID, msgID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to decrypt attachment"})
			return
		}
		full, ok := resolveAttachmentPath(convID, rel)
		if !ok {
			notFound(w)
			return
		}
		f, err := os.Open(full)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				notFound(w)
				return
			}
			log.Printf("familychat: attachment open conv=%d msg=%d: %v", convID, msgID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to open attachment"})
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			log.Printf("familychat: attachment stat conv=%d msg=%d: %v", convID, msgID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to stat attachment"})
			return
		}

		if mime != "" {
			w.Header().Set("Content-Type", mime)
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		// Prevent browsers from sniffing a different type that might enable
		// XSS via a HTML payload masquerading as another mime.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", "inline")
		w.Header().Set("Cache-Control", "private, max-age=0, no-cache")

		if _, err := io.Copy(w, f); err != nil {
			log.Printf("familychat: attachment copy conv=%d msg=%d: %v", convID, msgID, err)
			return
		}
	}
}

// mimeForStoredUpload returns the MIME type for the stored upload identified by
// rel. It reads the .mime sidecar written by UploadAttachmentHandler; if the
// sidecar is absent (e.g. a legacy upload or a test that writes the file
// directly) it falls back to content sniffing via http.DetectContentType.
func mimeForStoredUpload(convID int64, rel string) (string, error) {
	full, ok := resolveAttachmentPath(convID, rel)
	if !ok {
		return "", fmt.Errorf("invalid attachment path %q", rel)
	}
	if data, err := os.ReadFile(full + ".mime"); err == nil {
		if mime := strings.TrimSpace(string(data)); mime != "" {
			return mime, nil
		}
	}
	// Sidecar absent — sniff from file header.
	f, err := os.Open(full)
	if err != nil {
		return "", fmt.Errorf("open attachment for sniff: %w", err)
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := io.ReadFull(f, buf)
	detected := http.DetectContentType(buf[:n])
	if idx := strings.Index(detected, ";"); idx >= 0 {
		detected = strings.TrimSpace(detected[:idx])
	}
	return detected, nil
}

// DeleteAttachmentDir removes the entire per-conversation attachment directory,
// including all uploaded files and their .mime sidecars. Called when a
// conversation is deleted. Errors are logged but not propagated — the DB
// cascade has already removed the message rows.
func DeleteAttachmentDir(convID int64) {
	dir := filepath.Join(attachmentRoot(), strconv.FormatInt(convID, 10))
	if err := os.RemoveAll(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("familychat: remove attachment dir conv=%d: %v", convID, err)
	}
}

// knownAttachmentUUIDs returns the set of UUID filenames referenced by messages
// in convID. Since attachment_path is AES-GCM encrypted with a random nonce it
// cannot be matched directly in SQL; all non-empty paths are loaded and
// decrypted in the application layer.
func knownAttachmentUUIDs(db *sql.DB, convID int64) (map[string]struct{}, error) {
	rows, err := db.Query(
		`SELECT attachment_path FROM family_chat_messages WHERE conversation_id = ? AND attachment_path <> ''`,
		convID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := make(map[string]struct{})
	for rows.Next() {
		var encPath string
		if err := rows.Scan(&encPath); err != nil {
			return nil, err
		}
		rel, decErr := encryption.DecryptField(encPath)
		if decErr != nil {
			continue // Skip corrupted rows; keep scanning for valid ones.
		}
		if rel != "" {
			known[rel] = struct{}{}
		}
	}
	return known, rows.Err()
}

// CleanOrphanedAttachments removes uploaded files that have no corresponding
// message row in the DB and are older than minAge. The grace period prevents
// a race with the two-phase upload-then-send flow: files created within minAge
// are left alone so an in-flight send can still reference them.
func CleanOrphanedAttachments(db *sql.DB, minAge time.Duration) {
	root := attachmentRoot()
	convEntries, err := os.ReadDir(root)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("familychat: orphan sweep readdir %s: %v", root, err)
		}
		return
	}
	for _, convEntry := range convEntries {
		if !convEntry.IsDir() {
			continue
		}
		convID, parseErr := strconv.ParseInt(convEntry.Name(), 10, 64)
		if parseErr != nil {
			continue
		}
		convDir := filepath.Join(root, convEntry.Name())
		files, readErr := os.ReadDir(convDir)
		if readErr != nil {
			log.Printf("familychat: orphan sweep readdir %s: %v", convDir, readErr)
			continue
		}
		known, queryErr := knownAttachmentUUIDs(db, convID)
		if queryErr != nil {
			log.Printf("familychat: orphan sweep query conv=%d: %v", convID, queryErr)
			continue
		}
		for _, f := range files {
			if f.IsDir() || strings.HasSuffix(f.Name(), ".mime") {
				continue
			}
			info, infoErr := f.Info()
			if infoErr != nil || time.Since(info.ModTime()) < minAge {
				continue
			}
			uuid := f.Name()
			if _, referenced := known[uuid]; referenced {
				continue
			}
			full := filepath.Join(convDir, uuid)
			_ = os.Remove(full)
			_ = os.Remove(full + ".mime")
			log.Printf("familychat: orphan sweep deleted conv=%d uuid=%s", convID, uuid)
		}
	}
}

// StartOrphanSweep runs CleanOrphanedAttachments on an immediate call and then
// every interval in a background goroutine. The goroutine exits when ctx is
// cancelled — tie this to the server shutdown context so the sweep stops
// cleanly before the DB is closed.
func StartOrphanSweep(ctx context.Context, db *sql.DB, interval, minAge time.Duration) {
	go func() {
		CleanOrphanedAttachments(db, minAge)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				CleanOrphanedAttachments(db, minAge)
			}
		}
	}()
}
