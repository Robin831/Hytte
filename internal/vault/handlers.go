package vault

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON encode error: %v", err)
	}
}

// UploadHandler handles file uploads via multipart form.
func UploadHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, maxFileSize+4096)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "file too large"})
			} else {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form"})
			}
			return
		}
		defer r.MultipartForm.RemoveAll()

		file, header, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no file provided"})
			return
		}
		defer file.Close()

		data, err := ReadFileData(file)
		if err != nil {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "file too large"})
			return
		}

		filename := strings.TrimSpace(header.Filename)
		if filename == "" {
			filename = "unnamed"
		}

		mimeType := header.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}

		folder := strings.TrimSpace(r.FormValue("folder"))
		access := strings.TrimSpace(r.FormValue("access"))
		if access == "" {
			access = "private"
		}
		if access != "private" && access != "shared" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "access must be 'private' or 'shared'"})
			return
		}

		var tags []string
		if tagsStr := strings.TrimSpace(r.FormValue("tags")); tagsStr != "" {
			for _, t := range strings.Split(tagsStr, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					tags = append(tags, t)
				}
			}
		}
		if tags == nil {
			tags = []string{}
		}

		vf, err := Create(db, user.ID, filename, mimeType, folder, access, tags, data)
		if err != nil {
			log.Printf("Failed to create vault file: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to upload file"})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"file": vf})
	}
}

// ListHandler returns all vault files for the authenticated user.
func ListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		folder := strings.TrimSpace(r.URL.Query().Get("folder"))
		tag := strings.TrimSpace(r.URL.Query().Get("tag"))
		search := strings.TrimSpace(r.URL.Query().Get("search"))

		files, err := List(db, user.ID, folder, tag, search)
		if err != nil {
			log.Printf("Failed to list vault files: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list files"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"files": files})
	}
}

// GetHandler returns metadata for a single vault file.
func GetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		fileID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid file ID"})
			return
		}

		f, err := Get(db, user.ID, fileID)
		if err != nil {
			log.Printf("Failed to get vault file: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get file"})
			return
		}
		if f == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"file": f})
	}
}

// DownloadHandler serves the decrypted file content for download.
func DownloadHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		fileID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid file ID"})
			return
		}

		f, err := Get(db, user.ID, fileID)
		if err != nil {
			log.Printf("Failed to get vault file for download: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get file"})
			return
		}
		if f == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
			return
		}

		data, err := Download(user.ID, fileID)
		if err != nil {
			log.Printf("Failed to download vault file: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to download file"})
			return
		}

		w.Header().Set("Content-Type", f.MimeType)
		w.Header().Set("Content-Disposition", "attachment; filename=\""+f.Filename+"\"")
		w.Header().Set("Content-Length", strconv.FormatInt(int64(len(data)), 10))
		w.Write(data)
	}
}

// PreviewHandler serves the decrypted file content inline for preview.
func PreviewHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		fileID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid file ID"})
			return
		}

		data, mimeType, err := PreviewData(db, user.ID, fileID)
		if err != nil {
			if err.Error() == "file type not previewable" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file type not previewable"})
				return
			}
			log.Printf("Failed to preview vault file: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to preview file"})
			return
		}
		if data == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
			return
		}

		w.Header().Set("Content-Type", mimeType)
		w.Header().Set("Content-Disposition", "inline")
		w.Header().Set("Content-Length", strconv.FormatInt(int64(len(data)), 10))
		w.Write(data)
	}
}

// UpdateHandler modifies file metadata (filename, folder, access, tags).
func UpdateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		fileID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid file ID"})
			return
		}

		var body struct {
			Filename string   `json:"filename"`
			Folder   string   `json:"folder"`
			Access   string   `json:"access"`
			Tags     []string `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		body.Filename = strings.TrimSpace(body.Filename)
		if body.Filename == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "filename is required"})
			return
		}
		if body.Access != "private" && body.Access != "shared" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "access must be 'private' or 'shared'"})
			return
		}
		if body.Tags == nil {
			body.Tags = []string{}
		}

		f, err := Update(db, user.ID, fileID, body.Filename, body.Folder, body.Access, body.Tags)
		if err != nil {
			log.Printf("Failed to update vault file: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update file"})
			return
		}
		if f == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"file": f})
	}
}

// DeleteHandler removes a vault file.
func DeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		fileID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid file ID"})
			return
		}

		if err := Delete(db, user.ID, fileID); err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
				return
			}
			log.Printf("Failed to delete vault file: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete file"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// FoldersHandler returns all folder names for the user's vault.
func FoldersHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		folders, err := ListFolders(db, user.ID)
		if err != nil {
			log.Printf("Failed to list vault folders: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list folders"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"folders": folders})
	}
}

// TagsHandler returns all tags for the user's vault files.
func TagsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		tags, err := ListTags(db, user.ID)
		if err != nil {
			log.Printf("Failed to list vault tags: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list tags"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
	}
}
