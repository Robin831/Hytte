package trips

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// ImportFile is a trip in the import format (see cmd/trips-import): the
// document plus core fields and checklists with their items.
type ImportFile struct {
	Doc
	Kind       string `json:"kind"`
	StartDate  string `json:"start_date"`
	EndDate    string `json:"end_date"`
	HomeTZ     string `json:"home_tz"`
	DestTZ     string `json:"dest_tz"`
	Checklists []struct {
		Phase string `json:"phase"`
		Title I18n   `json:"title"`
		Items []struct {
			Title  I18n `json:"title"`
			Detail I18n `json:"detail"`
			Done   bool `json:"done"`
			Urgent bool `json:"urgent"`
		} `json:"items"`
	} `json:"checklists"`
}

// ImportTrip creates a trip owned by ownerID from an import file. Travellers
// whose first name matches a Hytte user become members; a race trip is
// linked to the owner's hall-of-fame result for that race day.
func ImportTrip(ctx context.Context, db *sql.DB, ownerID int64, f ImportFile) (int64, error) {
	users := map[string]int64{}
	rows, err := db.QueryContext(ctx, `SELECT id, name FROM users WHERE id > 0`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id int64
		var name string
		if rows.Scan(&id, &name) == nil {
			if first := strings.Fields(name); len(first) > 0 {
				users[strings.ToLower(first[0])] = id
			}
		}
	}
	rows.Close()
	for i := range f.Travellers {
		if id, ok := users[strings.ToLower(strings.TrimSpace(f.Travellers[i].Name))]; ok {
			id := id
			f.Travellers[i].UserID = &id
		}
	}

	in := TripInput{Kind: f.Kind, StartDate: f.StartDate, EndDate: f.EndDate, HomeTZ: f.HomeTZ, DestTZ: f.DestTZ, Doc: f.Doc}
	if f.Race != nil && f.Race.Date != "" {
		var resultID int64
		if err := db.QueryRowContext(ctx, `SELECT id FROM race_results WHERE user_id = ? AND person_name = '' AND race_date = ?
			ORDER BY id LIMIT 1`, ownerID, f.Race.Date).Scan(&resultID); err == nil {
			in.ResultID = &resultID
		}
	}
	tripID, err := CreateTrip(ctx, db, ownerID, in)
	if err != nil {
		return 0, err
	}
	for _, c := range f.Checklists {
		gid, err := AddChecklist(ctx, db, tripID, ownerID, ChecklistInput{Phase: c.Phase, Title: c.Title})
		if err != nil {
			return tripID, fmt.Errorf("checklist: %w", err)
		}
		for _, it := range c.Items {
			iid, err := AddItem(ctx, db, gid, ownerID, ItemInput{Title: it.Title, Detail: it.Detail, Urgent: it.Urgent})
			if err != nil {
				return tripID, fmt.Errorf("checklist item: %w", err)
			}
			if it.Done {
				if _, err := db.ExecContext(ctx, `UPDATE trip_check_items SET done = 1, done_at = ? WHERE id = ?`, now(), iid); err != nil {
					return tripID, err
				}
			}
		}
	}
	return tripID, nil
}
