package allowance

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

const (
	maxPhotoSize    = 10 << 20 // 10 MB
	photoMaxAgeDays = 7
)

// chorePhotosDir is the directory where chore completion photos are stored.
// It is a var (not const) so tests can override it with a temporary directory.
var chorePhotosDir = "data/chore-photos"

// SetCompletionPhotoPath stores the photo file path for a completion record.
func SetCompletionPhotoPath(db *sql.DB, completionID int64, photoPath string) error {
	_, err := db.Exec(
		`UPDATE allowance_completions SET photo_path = ? WHERE id = ?`,
		photoPath, completionID,
	)
	return err
}

// getCompletionPhotoAccess returns the photo path for the given completion if
// the user is either the child who created it or a parent who owns the chore.
// Returns ("", nil) when the completion has no photo.
// Returns ErrCompletionNotFound when the completion does not exist or the user
// is not authorised (to avoid leaking existence).
func getCompletionPhotoAccess(db *sql.DB, completionID, userID int64) (string, error) {
	var photoPath sql.NullString
	var childID, parentID int64
	err := db.QueryRow(`
		SELECT comp.photo_path, comp.child_id, c.parent_id
		FROM allowance_completions comp
		JOIN allowance_chores c ON c.id = comp.chore_id
		WHERE comp.id = ?
	`, completionID).Scan(&photoPath, &childID, &parentID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrCompletionNotFound
	}
	if err != nil {
		return "", err
	}
	if userID != childID && userID != parentID {
		return "", ErrCompletionNotFound
	}
	if !photoPath.Valid || photoPath.String == "" {
		return "", nil
	}
	return photoPath.String, nil
}

// saveChorePhoto writes r to disk as data/chore-photos/{completionID}.jpg and
// returns the stored path. Returns an error if the photo exceeds maxPhotoSize.
func saveChorePhoto(completionID int64, r io.Reader) (string, error) {
	if err := os.MkdirAll(chorePhotosDir, 0755); err != nil {
		return "", fmt.Errorf("create photos dir: %w", err)
	}
	fileName := fmt.Sprintf("%d.jpg", completionID)
	filePath := filepath.Join(chorePhotosDir, fileName)
	f, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("create photo file: %w", err)
	}
	defer f.Close()

	// Use N+1 to detect overflow: if lr.N reaches 0 the file exceeded maxPhotoSize.
	lr := &io.LimitedReader{R: r, N: maxPhotoSize + 1}
	if _, err := io.Copy(f, lr); err != nil {
		os.Remove(filePath) //nolint:errcheck
		return "", fmt.Errorf("write photo: %w", err)
	}
	if lr.N == 0 {
		os.Remove(filePath) //nolint:errcheck
		return "", fmt.Errorf("photo exceeds maximum size of %d bytes", maxPhotoSize)
	}
	return filePath, nil
}

// ServePhotoHandler serves the photo file associated with a completion.
// GET /api/allowance/photos/{completion_id}
func ServePhotoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		completionID, err := strconv.ParseInt(chi.URLParam(r, "completion_id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResponse("invalid completion ID"))
			return
		}

		photoPath, err := getCompletionPhotoAccess(db, completionID, user.ID)
		if errors.Is(err, ErrCompletionNotFound) {
			writeJSON(w, http.StatusNotFound, errResponse("completion not found"))
			return
		}
		if err != nil {
			log.Printf("allowance: serve photo completion %d user %d: %v", completionID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to retrieve photo"))
			return
		}
		if photoPath == "" {
			writeJSON(w, http.StatusNotFound, errResponse("no photo for this completion"))
			return
		}

		f, err := os.Open(photoPath)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusNotFound, errResponse("photo file not found"))
				return
			}
			log.Printf("allowance: open photo %s: %v", photoPath, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to read photo"))
			return
		}
		defer f.Close()

		// Detect content type from first 512 bytes, then seek back.
		buf := make([]byte, 512)
		n, _ := f.Read(buf)
		contentType := http.DetectContentType(buf[:n])
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			log.Printf("allowance: seek photo %s: %v", photoPath, err)
			writeJSON(w, http.StatusInternalServerError, errResponse("failed to read photo"))
			return
		}

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "private, max-age=86400")
		io.Copy(w, f) //nolint:errcheck
	}
}

// CleanOldCompletionPhotos deletes photo files and clears photo_path for
// completions created more than 7 days ago. Missing files are ignored.
// Safe to call repeatedly.
func CleanOldCompletionPhotos(db *sql.DB) {
	cutoff := time.Now().UTC().AddDate(0, 0, -photoMaxAgeDays).Format(time.RFC3339)
	rows, err := db.Query(`
		SELECT id, photo_path FROM allowance_completions
		WHERE photo_path IS NOT NULL AND photo_path != '' AND created_at < ?
	`, cutoff)
	if err != nil {
		log.Printf("allowance: clean photos query: %v", err)
		return
	}
	defer rows.Close()

	type entry struct {
		id   int64
		path string
	}
	var toClean []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.path); err != nil {
			log.Printf("allowance: clean photos scan: %v", err)
			continue
		}
		toClean = append(toClean, e)
	}
	if err := rows.Err(); err != nil {
		log.Printf("allowance: clean photos rows: %v", err)
		return
	}

	for _, e := range toClean {
		if err := os.Remove(e.path); err != nil && !os.IsNotExist(err) {
			log.Printf("allowance: remove photo %s: %v", e.path, err)
		}
		if _, dbErr := db.Exec(
			`UPDATE allowance_completions SET photo_path = '' WHERE id = ?`, e.id,
		); dbErr != nil {
			log.Printf("allowance: clear photo_path completion %d: %v", e.id, dbErr)
		}
	}
	if len(toClean) > 0 {
		log.Printf("allowance: cleaned %d old completion photos", len(toClean))
	}
}
