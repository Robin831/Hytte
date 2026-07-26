package wardrobe

import (
	"database/sql"
	"encoding/json"
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

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func urlID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil && id > 0
}

// --- Kids ---

// HandleListKids returns the user's kid profiles with derived size guidance.
func HandleListKids(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		kids, err := ListKids(db, user.ID)
		if err != nil {
			log.Printf("wardrobe: list kids: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to list kids")
			return
		}
		out := []KidWithStats{}
		for _, k := range kids {
			stats, err := KidStats(db, k)
			if err != nil {
				log.Printf("wardrobe: kid stats: %v", err)
				writeError(w, http.StatusInternalServerError, "failed to compute kid stats")
				return
			}
			out = append(out, stats)
		}
		writeJSON(w, http.StatusOK, map[string]any{"kids": out})
	}
}

// HandleAddKid creates a kid profile.
func HandleAddKid(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var body KidRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if body.Birthdate != "" && normalizeDate(body.Birthdate) == "" {
			writeError(w, http.StatusBadRequest, "birthdate must be YYYY-MM-DD")
			return
		}
		kid, err := AddKid(db, Kid{
			ParentID:    user.ID,
			Name:        body.Name,
			Birthdate:   normalizeDate(body.Birthdate),
			AvatarEmoji: body.AvatarEmoji,
		})
		if err != nil {
			log.Printf("wardrobe: add kid: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to add kid")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"kid": kid})
	}
}

// HandleUpdateKid updates a kid profile.
func HandleUpdateKid(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := urlID(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid kid ID")
			return
		}
		var body KidRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if body.Birthdate != "" && normalizeDate(body.Birthdate) == "" {
			writeError(w, http.StatusBadRequest, "birthdate must be YYYY-MM-DD")
			return
		}
		body.Birthdate = normalizeDate(body.Birthdate)
		if err := UpdateKid(db, id, user.ID, body); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "kid not found")
				return
			}
			log.Printf("wardrobe: update kid: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to update kid")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// HandleDeleteKid deletes a kid profile and, via cascade, its measurements and items.
func HandleDeleteKid(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := urlID(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid kid ID")
			return
		}
		if err := DeleteKid(db, id, user.ID); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "kid not found")
				return
			}
			log.Printf("wardrobe: delete kid: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to delete kid")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// --- Measurements ---

// HandleListMeasurements returns a kid's measurement history, oldest first.
func HandleListMeasurements(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		kidID, ok := urlID(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid kid ID")
			return
		}
		owned, err := kidOwnedBy(db, kidID, user.ID)
		if err != nil {
			log.Printf("wardrobe: check kid: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to list measurements")
			return
		}
		if !owned {
			writeError(w, http.StatusNotFound, "kid not found")
			return
		}
		ms, err := ListMeasurements(db, kidID)
		if err != nil {
			log.Printf("wardrobe: list measurements: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to list measurements")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"measurements": ms})
	}
}

// HandleAddMeasurement records a new measurement for a kid.
func HandleAddMeasurement(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		kidID, ok := urlID(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid kid ID")
			return
		}
		owned, err := kidOwnedBy(db, kidID, user.ID)
		if err != nil {
			log.Printf("wardrobe: check kid: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to add measurement")
			return
		}
		if !owned {
			writeError(w, http.StatusNotFound, "kid not found")
			return
		}
		var body MeasurementRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		measuredAt := normalizeDate(body.MeasuredAt)
		if measuredAt == "" {
			writeError(w, http.StatusBadRequest, "measured_at must be YYYY-MM-DD")
			return
		}
		if body.HeightCM < 0 || body.HeightCM > 250 || body.FootLengthMM < 0 || body.FootLengthMM > 400 || body.WeightKG < 0 || body.WeightKG > 200 {
			writeError(w, http.StatusBadRequest, "measurement out of range")
			return
		}
		if body.HeightCM == 0 && body.FootLengthMM == 0 && body.WeightKG == 0 {
			writeError(w, http.StatusBadRequest, "at least one measurement is required")
			return
		}
		m, err := AddMeasurement(db, Measurement{
			KidID:        kidID,
			MeasuredAt:   measuredAt,
			HeightCM:     body.HeightCM,
			FootLengthMM: body.FootLengthMM,
			WeightKG:     body.WeightKG,
			Note:         strings.TrimSpace(body.Note),
		})
		if err != nil {
			log.Printf("wardrobe: add measurement: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to add measurement")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"measurement": m})
	}
}

// HandleDeleteMeasurement removes a measurement.
func HandleDeleteMeasurement(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := urlID(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid measurement ID")
			return
		}
		if err := DeleteMeasurement(db, id, user.ID); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "measurement not found")
				return
			}
			log.Printf("wardrobe: delete measurement: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to delete measurement")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// --- Categories ---

// HandleListCategories returns the user's categories, seeding the defaults on
// first use so the page is never empty.
func HandleListCategories(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		cats, err := ListCategories(db, user.ID)
		if err != nil {
			log.Printf("wardrobe: list categories: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to list categories")
			return
		}
		if len(cats) == 0 {
			if err := SeedDefaultCategories(db, user.ID); err != nil {
				log.Printf("wardrobe: seed categories: %v", err)
				writeError(w, http.StatusInternalServerError, "failed to seed categories")
				return
			}
			if cats, err = ListCategories(db, user.ID); err != nil {
				log.Printf("wardrobe: list categories after seed: %v", err)
				writeError(w, http.StatusInternalServerError, "failed to list categories")
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"categories": cats})
	}
}

func validateCategoryRequest(body *CategoryRequest) string {
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		return "name is required"
	}
	if body.SizeSystem == "" {
		body.SizeSystem = "none"
	}
	if !validOneOf(body.SizeSystem, "clothing", "shoe", "none") {
		return "invalid size_system"
	}
	if body.TargetQty < 0 || body.TargetQty > 99 {
		return "target_qty must be between 0 and 99"
	}
	return ""
}

// HandleAddCategory creates a custom category.
func HandleAddCategory(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var body CategoryRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if msg := validateCategoryRequest(&body); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		cat, err := AddCategory(db, Category{
			ParentID:   user.ID,
			Name:       body.Name,
			Icon:       body.Icon,
			SizeSystem: body.SizeSystem,
			TargetQty:  body.TargetQty,
		})
		if err != nil {
			log.Printf("wardrobe: add category: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to add category")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"category": cat})
	}
}

// HandleUpdateCategory updates a category (name, icon, size system, target).
func HandleUpdateCategory(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := urlID(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid category ID")
			return
		}
		var body CategoryRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if msg := validateCategoryRequest(&body); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		if err := UpdateCategory(db, id, user.ID, body); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "category not found")
				return
			}
			log.Printf("wardrobe: update category: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to update category")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// HandleDeleteCategory deletes a category with no items.
func HandleDeleteCategory(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := urlID(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid category ID")
			return
		}
		if err := DeleteCategory(db, id, user.ID); err != nil {
			switch err {
			case sql.ErrNoRows:
				writeError(w, http.StatusNotFound, "category not found")
			case ErrCategoryInUse:
				writeError(w, http.StatusConflict, "category has items")
			default:
				log.Printf("wardrobe: delete category: %v", err)
				writeError(w, http.StatusInternalServerError, "failed to delete category")
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// --- Items ---

// HandleListItems returns the user's items, optionally filtered by ?kid_id=.
func HandleListItems(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var kidID int64
		if raw := r.URL.Query().Get("kid_id"); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed <= 0 {
				writeError(w, http.StatusBadRequest, "invalid kid_id")
				return
			}
			kidID = parsed
		}
		items, err := ListItems(db, user.ID, kidID)
		if err != nil {
			log.Printf("wardrobe: list items: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to list items")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func validateItemRequest(db *sql.DB, parentID int64, body *ItemRequest) (string, error) {
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		return "name is required", nil
	}
	if body.KidID <= 0 {
		return "kid_id is required", nil
	}
	owned, err := kidOwnedBy(db, body.KidID, parentID)
	if err != nil {
		return "", err
	}
	if !owned {
		return "kid not found", nil
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM wardrobe_categories WHERE id = ? AND parent_id = ?", body.CategoryID, parentID).Scan(&n); err != nil {
		return "", err
	}
	if n == 0 {
		return "category not found", nil
	}
	if body.Quantity < 1 {
		body.Quantity = 1
	}
	if body.Quantity > 99 {
		return "quantity must be at most 99", nil
	}
	if body.Condition == "" {
		body.Condition = "good"
	}
	if body.Status == "" {
		body.Status = "active"
	}
	if body.Location == "" {
		body.Location = "home"
	}
	if body.Season == "" {
		body.Season = "all"
	}
	if !validOneOf(body.Condition, "new", "good", "worn") {
		return "invalid condition", nil
	}
	if !validOneOf(body.Status, "active", "too_small", "stored") {
		return "invalid status", nil
	}
	if !validOneOf(body.Location, "home", "kindergarten", "school", "cabin", "other") {
		return "invalid location", nil
	}
	if !validOneOf(body.Season, "all", "summer", "winter") {
		return "invalid season", nil
	}
	return "", nil
}

// HandleAddItem creates a wardrobe item.
func HandleAddItem(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var body ItemRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		msg, err := validateItemRequest(db, user.ID, &body)
		if err != nil {
			log.Printf("wardrobe: validate item: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to add item")
			return
		}
		if msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		item, err := AddItem(db, Item{
			ParentID:   user.ID,
			KidID:      body.KidID,
			CategoryID: body.CategoryID,
			Name:       body.Name,
			SizeLabel:  strings.TrimSpace(body.SizeLabel),
			Quantity:   body.Quantity,
			Condition:  body.Condition,
			Status:     body.Status,
			Location:   body.Location,
			Season:     body.Season,
			Notes:      strings.TrimSpace(body.Notes),
		})
		if err != nil {
			log.Printf("wardrobe: add item: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to add item")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"item": item})
	}
}

// HandleUpdateItem updates a wardrobe item.
func HandleUpdateItem(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := urlID(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid item ID")
			return
		}
		var body ItemRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		msg, err := validateItemRequest(db, user.ID, &body)
		if err != nil {
			log.Printf("wardrobe: validate item: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to update item")
			return
		}
		if msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		body.SizeLabel = strings.TrimSpace(body.SizeLabel)
		body.Notes = strings.TrimSpace(body.Notes)
		if err := UpdateItem(db, id, user.ID, body); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "item not found")
				return
			}
			log.Printf("wardrobe: update item: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to update item")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// HandleDeleteItem removes a wardrobe item.
func HandleDeleteItem(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := urlID(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid item ID")
			return
		}
		if err := DeleteItem(db, id, user.ID); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "item not found")
				return
			}
			log.Printf("wardrobe: delete item: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to delete item")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// --- Needs ---

// HandleNeeds returns a kid's computed wardrobe gaps.
func HandleNeeds(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		kidID, err := strconv.ParseInt(r.URL.Query().Get("kid_id"), 10, 64)
		if err != nil || kidID <= 0 {
			writeError(w, http.StatusBadRequest, "invalid kid_id")
			return
		}
		owned, err := kidOwnedBy(db, kidID, user.ID)
		if err != nil {
			log.Printf("wardrobe: check kid: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to compute needs")
			return
		}
		if !owned {
			writeError(w, http.StatusNotFound, "kid not found")
			return
		}
		needs, tooSmall, err := Needs(db, user.ID, kidID)
		if err != nil {
			log.Printf("wardrobe: needs: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to compute needs")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"needs": needs, "too_small": tooSmall})
	}
}
