package trips

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/races"
	"github.com/Robin831/Hytte/internal/training"
)

// Pictures in the trip chat. A picture is stored encrypted with its message
// (and goes when the message goes). Before the assistant answers, each new
// picture is read by a separate vision call that only has file access to a
// directory holding that one picture; its description is what the main,
// web-enabled call sees. Keeping the two apart means the call that can
// browse the web never has file access.

const (
	maxChatImages     = 4
	maxChatImageBytes = 3 << 20 // after the browser has scaled it down
)

var chatImageTypes = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif"}

// ImageInput is a picture as the browser sends it.
type ImageInput struct {
	Data string `json:"data"` // base64, with or without a data: URL prefix
}

type decodedImage struct {
	mime string
	data []byte
}

func decodeImages(in []ImageInput) ([]decodedImage, error) {
	if len(in) > maxChatImages {
		return nil, invalid("at most %d pictures per message", maxChatImages)
	}
	var out []decodedImage
	for _, im := range in {
		raw := im.Data
		if i := strings.Index(raw, ","); strings.HasPrefix(raw, "data:") && i > 0 {
			raw = raw[i+1:]
		}
		data, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(data) == 0 {
			return nil, invalid("a picture could not be read")
		}
		if len(data) > maxChatImageBytes {
			return nil, invalid("a picture is too large (max %d MB)", maxChatImageBytes>>20)
		}
		// Trust the bytes, not the label.
		mime := http.DetectContentType(data)
		if _, ok := chatImageTypes[mime]; !ok {
			return nil, invalid("pictures must be JPEG, PNG, WebP or GIF")
		}
		out = append(out, decodedImage{mime: mime, data: data})
	}
	return out, nil
}

func saveImages(ctx context.Context, db *sql.DB, tripID, messageID int64, images []decodedImage) error {
	for _, im := range images {
		enc, err := encryption.EncryptField(base64.StdEncoding.EncodeToString(im.data))
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO trip_chat_images (trip_id, message_id, mime, data, created_at)
			VALUES (?, ?, ?, ?, ?)`, tripID, messageID, im.mime, enc, now()); err != nil {
			return fmt.Errorf("save chat picture: %w", err)
		}
	}
	return nil
}

// ChatImage returns a picture's type and bytes to a user who can see the trip.
func ChatImage(ctx context.Context, db *sql.DB, tripID, imageID, userID int64) (string, []byte, error) {
	canView, _, err := access(ctx, db, tripID, userID)
	if err != nil {
		return "", nil, err
	}
	if !canView {
		return "", nil, ErrNotFound
	}
	var mime, enc string
	if err := db.QueryRowContext(ctx, `SELECT mime, data FROM trip_chat_images WHERE id = ? AND trip_id = ?`, imageID, tripID).Scan(&mime, &enc); err == sql.ErrNoRows {
		return "", nil, ErrNotFound
	} else if err != nil {
		return "", nil, err
	}
	b64, err := encryption.DecryptField(enc)
	if err != nil {
		return "", nil, err
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	return mime, data, err
}

const describePrompt = `This picture was sent to a family's trip-planning assistant. Describe it so the assistant can use it without seeing it:
- Transcribe every piece of text that matters exactly as written: names of hotels, airlines and places, dates and times, prices with currency, booking or reference numbers, addresses, phone numbers, flight numbers, room types.
- Then say in one or two sentences what the picture is (a booking confirmation, a hotel listing, a screenshot of a map, a photo of a sign, a boarding pass...).
Be factual and complete but compact; no commentary. If text is unreadable, say which part.`

// describeImages reads the pictures on the newest question that have no
// description yet, and returns descriptions for every picture in history.
func (c *Chat) describeImages(ctx context.Context, cfg *training.ClaudeConfig, tripID, userID int64, history []ChatMessage) map[int64]string {
	out := map[int64]string{}
	rows, err := c.DB.QueryContext(ctx, `SELECT id, description FROM trip_chat_images WHERE trip_id = ?`, tripID)
	if err != nil {
		return out
	}
	var missing []int64
	latest := map[int64]bool{}
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" {
			for _, id := range history[i].Images {
				latest[id] = true
			}
			break
		}
	}
	for rows.Next() {
		var id int64
		var enc string
		if rows.Scan(&id, &enc) != nil {
			continue
		}
		if enc != "" {
			out[id] = encryption.DecryptLenient(enc)
		} else if latest[id] {
			missing = append(missing, id)
		}
	}
	rows.Close()

	for _, id := range missing {
		desc, err := c.describeOne(ctx, cfg, tripID, userID, id)
		if err != nil {
			log.Printf("trips: read chat picture %d: %v", id, err)
			desc = "(this picture could not be read)"
		}
		out[id] = desc
		if enc, eerr := encryption.EncryptField(desc); eerr == nil {
			_, _ = c.DB.ExecContext(ctx, `UPDATE trip_chat_images SET description = ? WHERE id = ?`, enc, id)
		}
	}
	return out
}

func (c *Chat) describeOne(ctx context.Context, cfg *training.ClaudeConfig, tripID, userID, imageID int64) (string, error) {
	mime, data, err := ChatImage(ctx, c.DB, tripID, imageID, userID)
	if err != nil {
		return "", err
	}
	// A directory of its own: the vision call may read everything in it.
	dir, err := os.MkdirTemp("", "hytte-trip-picture-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "picture"+chatImageTypes[mime])
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	callCtx, cancel := context.WithTimeout(ctx, chatTimeout/2)
	defer cancel()
	text, err := c.Describe(callCtx, cfg, describePrompt, path)
	if err != nil {
		races.LogBackgroundRun(ctx, c.DB, "trip_chat", userID, 0, 0, "trip chat picture", err.Error())
		return "", err
	}
	text = strings.TrimSpace(text)
	if len([]rune(text)) > 3000 {
		text = string([]rune(text)[:3000]) + "…"
	}
	if text == "" {
		return "", fmt.Errorf("empty description")
	}
	return string(bytes.TrimSpace([]byte(text))), nil
}
