package vault

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// ErrFileTooLarge is returned when uploaded file data exceeds the size limit.
var ErrFileTooLarge = errors.New("file too large")

// ErrNotPreviewable is returned when a file type cannot be previewed inline.
var ErrNotPreviewable = errors.New("file type not previewable")

// maxFileSize is the maximum allowed upload size (50 MB).
const maxFileSize = 50 << 20

// File represents a vault file's metadata.
type File struct {
	ID        int64    `json:"id"`
	UserID    int64    `json:"user_id"`
	Filename  string   `json:"filename"`
	MimeType  string   `json:"mime_type"`
	SizeBytes int64    `json:"size_bytes"`
	Folder    string   `json:"folder"`
	Access    string   `json:"access"`
	Tags      []string `json:"tags"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// storageDir returns the base directory for vault file storage.
// Uses VAULT_STORAGE_DIR env var if set, otherwise ./data/vault.
func storageDir() string {
	if dir := os.Getenv("VAULT_STORAGE_DIR"); dir != "" {
		return dir
	}
	return filepath.Join("data", "vault")
}

// filePath returns the on-disk path for a vault file.
func filePath(userID, fileID int64) string {
	return filepath.Join(storageDir(), fmt.Sprintf("%d", userID), fmt.Sprintf("%d", fileID))
}

// validateStoragePath ensures the resolved path stays within the storage directory.
func validateStoragePath(path string) error {
	base, err := filepath.Abs(storageDir())
	if err != nil {
		return fmt.Errorf("resolve storage dir: %w", err)
	}
	resolved, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve file path: %w", err)
	}
	if !strings.HasPrefix(resolved, base+string(filepath.Separator)) {
		return fmt.Errorf("path escapes storage directory")
	}
	return nil
}

// Create stores a new file in the vault. The file content is encrypted before
// writing to disk. Metadata (with encrypted filename) is stored in SQLite.
func Create(db *sql.DB, userID int64, filename, mimeType, folder, access string, tags []string, data []byte) (*File, error) {
	if len(data) > maxFileSize {
		return nil, fmt.Errorf("file too large (max %d bytes)", maxFileSize)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	encFilename, err := encryption.EncryptField(filename)
	if err != nil {
		return nil, fmt.Errorf("encrypt filename: %w", err)
	}

	encData, err := encryption.Encrypt(string(data))
	if err != nil {
		return nil, fmt.Errorf("encrypt file data: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		INSERT INTO vault_files (user_id, filename, mime_type, size_bytes, folder, access, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, userID, encFilename, mimeType, len(data), folder, access, now, now)
	if err != nil {
		return nil, fmt.Errorf("insert vault file: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	if err := setTags(tx, id, tags); err != nil {
		return nil, err
	}

	// Write encrypted file to disk.
	diskPath := filePath(userID, id)
	if err := validateStoragePath(diskPath); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(diskPath), 0700); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
	}
	if err := os.WriteFile(diskPath, []byte(encData), 0600); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	if err := tx.Commit(); err != nil {
		// Clean up the written file on commit failure.
		os.Remove(diskPath)
		return nil, err
	}

	return &File{
		ID:        id,
		UserID:    userID,
		Filename:  filename,
		MimeType:  mimeType,
		SizeBytes: int64(len(data)),
		Folder:    folder,
		Access:    access,
		Tags:      tags,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// List returns all vault files for a user, optionally filtered by folder, tag, or search.
func List(db *sql.DB, userID int64, folder, tag, search string) ([]File, error) {
	query := `
		SELECT f.id, f.user_id, f.filename, f.mime_type, f.size_bytes, f.folder, f.access,
		       f.created_at, f.updated_at,
		       GROUP_CONCAT(ft.tag, char(31)) AS tags
		FROM vault_files f
		LEFT JOIN vault_file_tags ft ON ft.file_id = f.id
		WHERE f.user_id = ?`

	args := []any{userID}

	if folder != "" {
		query += ` AND f.folder = ?`
		args = append(args, folder)
	}

	if tag != "" {
		query += ` AND f.id IN (SELECT file_id FROM vault_file_tags WHERE tag = ?)`
		args = append(args, tag)
	}

	query += ` GROUP BY f.id ORDER BY f.updated_at DESC`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	searchLower := strings.ToLower(search)

	var files []File
	for rows.Next() {
		var f File
		var tagsStr sql.NullString
		if err := rows.Scan(&f.ID, &f.UserID, &f.Filename, &f.MimeType, &f.SizeBytes,
			&f.Folder, &f.Access, &f.CreatedAt, &f.UpdatedAt, &tagsStr); err != nil {
			return nil, err
		}

		if f.Filename, err = encryption.DecryptField(f.Filename); err != nil {
			return nil, fmt.Errorf("decrypt filename: %w", err)
		}

		if searchLower != "" && !strings.Contains(strings.ToLower(f.Filename), searchLower) {
			continue
		}

		if tagsStr.Valid && tagsStr.String != "" {
			f.Tags = strings.Split(tagsStr.String, "\x1f")
			sort.Strings(f.Tags)
		} else {
			f.Tags = []string{}
		}

		files = append(files, f)
	}

	if files == nil {
		files = []File{}
	}

	return files, rows.Err()
}

// Get returns a single vault file's metadata. Returns nil if not found or not owned by user.
func Get(db *sql.DB, userID, fileID int64) (*File, error) {
	var f File
	var tagsStr sql.NullString

	err := db.QueryRow(`
		SELECT f.id, f.user_id, f.filename, f.mime_type, f.size_bytes, f.folder, f.access,
		       f.created_at, f.updated_at,
		       GROUP_CONCAT(ft.tag, char(31)) AS tags
		FROM vault_files f
		LEFT JOIN vault_file_tags ft ON ft.file_id = f.id
		WHERE f.id = ? AND f.user_id = ?
		GROUP BY f.id
	`, fileID, userID).Scan(&f.ID, &f.UserID, &f.Filename, &f.MimeType, &f.SizeBytes,
		&f.Folder, &f.Access, &f.CreatedAt, &f.UpdatedAt, &tagsStr)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var decErr error
	if f.Filename, decErr = encryption.DecryptField(f.Filename); decErr != nil {
		return nil, fmt.Errorf("decrypt filename: %w", decErr)
	}

	if tagsStr.Valid && tagsStr.String != "" {
		f.Tags = strings.Split(tagsStr.String, "\x1f")
		sort.Strings(f.Tags)
	} else {
		f.Tags = []string{}
	}

	return &f, nil
}

// Download reads and decrypts a vault file's content from disk.
func Download(userID, fileID int64) ([]byte, error) {
	diskPath := filePath(userID, fileID)

	// Symlink protection: ensure the path is not a symlink.
	info, err := os.Lstat(diskPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("read file: refusing to follow symlink")
	}

	encData, err := os.ReadFile(diskPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	plaintext, err := encryption.Decrypt(string(encData))
	if err != nil {
		return nil, fmt.Errorf("decrypt file: %w", err)
	}

	return []byte(plaintext), nil
}

// Update modifies a vault file's metadata (filename, folder, access, tags).
func Update(db *sql.DB, userID, fileID int64, filename, folder, access string, tags []string) (*File, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	encFilename, err := encryption.EncryptField(filename)
	if err != nil {
		return nil, fmt.Errorf("encrypt filename: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		UPDATE vault_files SET filename = ?, folder = ?, access = ?, updated_at = ?
		WHERE id = ? AND user_id = ?
	`, encFilename, folder, access, now, fileID, userID)
	if err != nil {
		return nil, err
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, nil
	}

	if err := setTags(tx, fileID, tags); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return Get(db, userID, fileID)
}

// Delete removes a vault file from both the database and disk.
// The on-disk file is removed before the DB row so that a DB failure cannot
// leave an orphaned encrypted blob with no metadata reference.
func Delete(db *sql.DB, userID, fileID int64) error {
	// Verify ownership before touching anything on disk.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM vault_files WHERE id = ? AND user_id = ?`, fileID, userID).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}

	// Remove the on-disk file first. If removal fails (for reasons other than
	// the file not existing), return an error so the DB row is preserved and
	// the user can retry — this prevents orphaned encrypted blobs on disk.
	diskPath := filePath(userID, fileID)
	if err := os.Remove(diskPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove vault file from disk: %w", err)
	}

	_, err := db.Exec(`DELETE FROM vault_files WHERE id = ? AND user_id = ?`, fileID, userID)
	return err
}

// ListFolders returns all distinct folder names for a user's vault.
func ListFolders(db *sql.DB, userID int64) ([]string, error) {
	rows, err := db.Query(`
		SELECT DISTINCT folder FROM vault_files WHERE user_id = ? AND folder != '' ORDER BY folder
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}

	if folders == nil {
		folders = []string{}
	}

	return folders, rows.Err()
}

// ListTags returns all distinct tags for a user's vault files.
func ListTags(db *sql.DB, userID int64) ([]string, error) {
	rows, err := db.Query(`
		SELECT DISTINCT ft.tag
		FROM vault_file_tags ft
		JOIN vault_files f ON f.id = ft.file_id
		WHERE f.user_id = ?
		ORDER BY ft.tag
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}

	if tags == nil {
		tags = []string{}
	}

	return tags, rows.Err()
}

// setTags replaces all tags for a file within a transaction.
func setTags(tx *sql.Tx, fileID int64, tags []string) error {
	if _, err := tx.Exec(`DELETE FROM vault_file_tags WHERE file_id = ?`, fileID); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		if _, err := tx.Exec(`
			INSERT INTO vault_file_tags (file_id, tag) VALUES (?, ?)
			ON CONFLICT(file_id, tag) DO NOTHING
		`, fileID, tag); err != nil {
			return err
		}
	}
	return nil
}

// PreviewData reads a vault file and returns its decrypted content, but only
// for previewable types (images, PDFs). Returns nil for non-previewable types.
func PreviewData(db *sql.DB, userID, fileID int64) ([]byte, string, error) {
	f, err := Get(db, userID, fileID)
	if err != nil || f == nil {
		return nil, "", err
	}

	if !isPreviewable(f.MimeType) {
		return nil, "", ErrNotPreviewable
	}

	data, err := Download(userID, fileID)
	if err != nil {
		return nil, "", err
	}

	return data, f.MimeType, nil
}

// isPreviewable returns true if the MIME type supports safe inline preview.
// Only explicitly allowlisted image types and PDF are permitted; text/html,
// image/svg+xml, and other scriptable types are excluded to prevent XSS.
func isPreviewable(mimeType string) bool {
	switch mimeType {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp", "image/avif":
		return true
	case "application/pdf":
		return true
	default:
		return false
	}
}

// ReadFileData reads file content from an io.Reader with a size limit.
func ReadFileData(r io.Reader) ([]byte, error) {
	lr := io.LimitedReader{R: r, N: maxFileSize + 1}
	data, err := io.ReadAll(&lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxFileSize {
		return nil, ErrFileTooLarge
	}
	return data, nil
}
