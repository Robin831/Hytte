package grocery

import "time"

// GroceryItem represents a single item on a user's grocery list.
type GroceryItem struct {
	ID             int64     `json:"id"`
	UserID         int64     `json:"user_id"`
	Content        string    `json:"content"`
	OriginalText   string    `json:"original_text"`
	SourceLanguage string    `json:"source_language"`
	Checked        bool      `json:"checked"`
	SortOrder      int       `json:"sort_order"`
	AddedBy        int64     `json:"added_by"`
	CreatedAt      time.Time `json:"created_at"`
}

// AddItemRequest is the JSON body for creating a new grocery item.
type AddItemRequest struct {
	Content        string `json:"content"`
	SourceLanguage string `json:"source_language"`
}

// UpdateCheckedRequest is the JSON body for toggling an item's checked state.
type UpdateCheckedRequest struct {
	Checked bool `json:"checked"`
}

// UpdateSortOrderRequest is the JSON body for reordering an item.
type UpdateSortOrderRequest struct {
	SortOrder int `json:"sort_order"`
}
