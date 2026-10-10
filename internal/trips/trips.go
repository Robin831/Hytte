// Package trips is the family's trip planner: race weekends, family visits,
// holidays. A trip's content (flights, stays, day plans, contacts, notes) is
// one document, encrypted at rest because it holds booking references and
// phone numbers; checklist items are rows so several travellers can tick
// them at once, with who and when. Every text is kept in nb, en and th —
// what a user types in one language is translated into the others in the
// background.
package trips

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// Languages every trip text is kept in.
var Languages = []string{"nb", "en", "th"}

// I18n is one text in every language.
type I18n map[string]string

var (
	ErrNotFound   = errors.New("not found")
	ErrForbidden  = errors.New("not allowed")
	ErrValidation = errors.New("invalid input")

	validKinds = map[string]bool{"race": true, "family": true, "holiday": true, "work": true, "other": true}
	datePat    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	localPat   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$`)
	iataPat    = regexp.MustCompile(`^[A-Z]{3}$`)
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

// Doc is a trip's content.
type Doc struct {
	Title      I18n        `json:"title"`
	Summary    I18n        `json:"summary"`
	Route      []string    `json:"route"`
	Travellers []Traveller `json:"travellers"`
	Race       *Race       `json:"race,omitempty"`
	Phases     []Phase     `json:"phases"`
	Flights    []Flight    `json:"flights"`
	Stays      []Stay      `json:"stays"`
	Transport  []Transport `json:"transport"`
	Days       []Day       `json:"days"`
	Contacts   []Contact   `json:"contacts"`
	Documents  []Document  `json:"documents"`
	Notes      []Note      `json:"notes"`
	Followups  []Followup  `json:"followups"`
}

type Traveller struct {
	Name   string `json:"name"`
	UserID *int64 `json:"user_id,omitempty"`
	Child  bool   `json:"child"`
}

type Race struct {
	Name   string `json:"name"`
	Date   string `json:"date"`
	Bib    string `json:"bib"`
	Result string `json:"result"`
}

type Phase struct {
	Key   string `json:"key"`
	Title I18n   `json:"title"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type Flight struct {
	Phase      string `json:"phase"`
	Airline    string `json:"airline"`
	FlightNo   string `json:"flight_no"`
	From       string `json:"from"`
	FromName   string `json:"from_name"`
	To         string `json:"to"`
	ToName     string `json:"to_name"`
	DepLocal   string `json:"dep_local"`
	DepTZ      string `json:"dep_tz"`
	ArrLocal   string `json:"arr_local"`
	ArrTZ      string `json:"arr_tz"`
	BookingRef string `json:"booking_ref"`
	Seat       string `json:"seat"`
	Note       I18n   `json:"note"`
}

type Stay struct {
	Phase      string `json:"phase"`
	Name       string `json:"name"`
	Address    string `json:"address"`
	Phone      string `json:"phone"`
	CheckIn    string `json:"check_in"`
	CheckOut   string `json:"check_out"`
	TZ         string `json:"tz"`
	BookingRef string `json:"booking_ref"`
	Room       string `json:"room"`
	Price      string `json:"price"`
	Note       I18n   `json:"note"`
}

type Transport struct {
	Phase     string `json:"phase"`
	Title     I18n   `json:"title"`
	WhenLocal string `json:"when_local"`
	TZ        string `json:"tz"`
	Detail    I18n   `json:"detail"`
	Ref       string `json:"ref"`
	Phone     string `json:"phone"`
}

type Day struct {
	Date      string `json:"date"`
	Title     I18n   `json:"title"`
	Summary   I18n   `json:"summary"`
	Highlight bool   `json:"highlight"`
	Steps     []Step `json:"steps"`
}

type Step struct {
	Time  string `json:"time"`
	Label I18n   `json:"label"`
	Text  I18n   `json:"text"`
	Key   bool   `json:"key"`
}

type Contact struct {
	Label  I18n   `json:"label"`
	Value  string `json:"value"`
	Kind   string `json:"kind"`
	Urgent bool   `json:"urgent"`
	Note   I18n   `json:"note"`
}

type Document struct {
	Title  I18n   `json:"title"`
	Status string `json:"status"`
	Detail I18n   `json:"detail"`
}

type Note struct {
	Phase string `json:"phase"`
	Title I18n   `json:"title"`
	Body  I18n   `json:"body"`
}

type Followup struct {
	Title  I18n `json:"title"`
	Detail I18n `json:"detail"`
	Done   bool `json:"done"`
}

// Trip is a trip as the API returns it.
type Trip struct {
	ID          int64       `json:"id"`
	OwnerID     int64       `json:"owner_id"`
	Kind        string      `json:"kind"`
	StartDate   string      `json:"start_date"`
	EndDate     string      `json:"end_date"`
	HomeTZ      string      `json:"home_tz"`
	DestTZ      string      `json:"dest_tz"`
	ShareFamily bool        `json:"share_family"`
	RaceEventID *int64      `json:"race_event_id"`
	ResultID    *int64      `json:"result_id"`
	Doc         Doc         `json:"doc"`
	Checklists  []Checklist `json:"checklists"`
	CanEdit     bool        `json:"can_edit"`
	CreatedAt   string      `json:"created_at"`
	UpdatedAt   string      `json:"updated_at"`
}

// Checklist is a group of checklist items.
type Checklist struct {
	ID       int64       `json:"id"`
	Phase    string      `json:"phase"`
	Title    I18n        `json:"title"`
	Position int         `json:"position"`
	Items    []CheckItem `json:"items"`
}

// CheckItem is one tickable item.
type CheckItem struct {
	ID         int64  `json:"id"`
	Title      I18n   `json:"title"`
	Detail     I18n   `json:"detail"`
	Urgent     bool   `json:"urgent"`
	Done       bool   `json:"done"`
	DoneByName string `json:"done_by_name"`
	DoneAt     string `json:"done_at"`
	Position   int    `json:"position"`
}

// TripInput is what create/update accept.
type TripInput struct {
	Kind        string `json:"kind"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	HomeTZ      string `json:"home_tz"`
	DestTZ      string `json:"dest_tz"`
	ShareFamily bool   `json:"share_family"`
	RaceEventID *int64 `json:"race_event_id"`
	ResultID    *int64 `json:"result_id"`
	Doc         Doc    `json:"doc"`
}

func validTZ(tz string) bool {
	if tz == "" {
		return false
	}
	_, err := time.LoadLocation(tz)
	return err == nil
}

func trimI18n(t I18n) I18n {
	out := I18n{}
	for _, l := range Languages {
		if v := strings.TrimSpace(t[l]); v != "" {
			out[l] = v
		}
	}
	return out
}

func checkLocal(field, v, tz string) error {
	if v == "" {
		return nil
	}
	if !localPat.MatchString(v) {
		return invalid("%s must be YYYY-MM-DDTHH:MM", field)
	}
	if !validTZ(tz) {
		return invalid("%s needs a valid time zone", field)
	}
	return nil
}

// Normalize trims and validates a trip.
func (in *TripInput) Normalize() error {
	if in.HomeTZ == "" {
		in.HomeTZ = "Europe/Oslo"
	}
	if in.DestTZ == "" {
		in.DestTZ = in.HomeTZ
	}
	switch {
	case !validKinds[in.Kind]:
		return invalid("unknown kind %q", in.Kind)
	case !datePat.MatchString(in.StartDate) || !datePat.MatchString(in.EndDate) || in.EndDate < in.StartDate:
		return invalid("start_date and end_date must be YYYY-MM-DD, end on or after start")
	case !validTZ(in.HomeTZ) || !validTZ(in.DestTZ):
		return invalid("home_tz and dest_tz must be IANA time zones")
	}
	d := &in.Doc
	d.Title = trimI18n(d.Title)
	if len(d.Title) == 0 {
		return invalid("title is required")
	}
	// Route stops are airport codes ("BGO") or, where a trip continues past
	// the airports, place names ("Surin").
	for i, stop := range d.Route {
		stop = strings.TrimSpace(stop)
		if iataPat.MatchString(strings.ToUpper(stop)) {
			stop = strings.ToUpper(stop)
		}
		if stop == "" || len([]rune(stop)) > 40 {
			return invalid("route stops must be airport codes or short place names")
		}
		d.Route[i] = stop
	}
	for i := range d.Flights {
		f := &d.Flights[i]
		if err := checkLocal("flight departure", f.DepLocal, f.DepTZ); err != nil {
			return err
		}
		if err := checkLocal("flight arrival", f.ArrLocal, f.ArrTZ); err != nil {
			return err
		}
	}
	for i := range d.Stays {
		s := &d.Stays[i]
		if err := checkLocal("check-in", s.CheckIn, s.TZ); err != nil {
			return err
		}
		if err := checkLocal("check-out", s.CheckOut, s.TZ); err != nil {
			return err
		}
	}
	for i := range d.Transport {
		if err := checkLocal("transport time", d.Transport[i].WhenLocal, d.Transport[i].TZ); err != nil {
			return err
		}
	}
	for i := range d.Days {
		if !datePat.MatchString(d.Days[i].Date) {
			return invalid("day dates must be YYYY-MM-DD")
		}
	}
	for i := range d.Phases {
		p := d.Phases[i]
		if p.Key == "" || !datePat.MatchString(p.Start) || !datePat.MatchString(p.End) {
			return invalid("phases need a key and start/end dates")
		}
	}
	for _, t := range d.Travellers {
		if strings.TrimSpace(t.Name) == "" {
			return invalid("travellers need a name")
		}
	}
	return nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func encryptJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return encryption.EncryptField(string(b))
}

func decryptJSON(s string, v any) error {
	dec, err := encryption.DecryptField(s)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}
	return json.Unmarshal([]byte(dec), v)
}

// --- access ---------------------------------------------------------------

// access reports whether the user may see and edit a trip. Owners and
// members (Hytte users among the travellers) can edit; with family sharing
// on, everyone else with the trips feature can view.
func access(ctx context.Context, db *sql.DB, tripID, userID int64) (canView, canEdit bool, err error) {
	var owner int64
	var share bool
	if err := db.QueryRowContext(ctx, `SELECT owner_id, share_family FROM trips WHERE id = ?`, tripID).Scan(&owner, &share); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, false, ErrNotFound
		}
		return false, false, err
	}
	if owner == userID {
		return true, true, nil
	}
	var member int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM trip_members WHERE trip_id = ? AND user_id = ?`, tripID, userID).Scan(&member); err != nil {
		return false, false, err
	}
	if member > 0 {
		return true, true, nil
	}
	return share, false, nil
}

func syncMembers(ctx context.Context, q interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, tripID int64, d Doc) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM trip_members WHERE trip_id = ?`, tripID); err != nil {
		return err
	}
	for _, t := range d.Travellers {
		if t.UserID != nil {
			if _, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO trip_members (trip_id, user_id) VALUES (?, ?)`, tripID, *t.UserID); err != nil {
				return err
			}
		}
	}
	return nil
}

// --- trips ------------------------------------------------------------------

// Summary is a trip as listed.
type Summary struct {
	ID         int64    `json:"id"`
	Kind       string   `json:"kind"`
	Title      I18n     `json:"title"`
	StartDate  string   `json:"start_date"`
	EndDate    string   `json:"end_date"`
	DestTZ     string   `json:"dest_tz"`
	Route      []string `json:"route"`
	Travellers []string `json:"travellers"`
	Open       int      `json:"open"` // unticked checklist items
	Total      int      `json:"total"`
}

// ListTrips returns the trips the user can see, newest start first.
func ListTrips(ctx context.Context, db *sql.DB, userID int64) ([]Summary, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT t.id, t.kind, t.start_date, t.end_date, t.dest_tz, t.doc,
		       (SELECT COUNT(*) FROM trip_check_items i WHERE i.trip_id = t.id AND i.done = 0),
		       (SELECT COUNT(*) FROM trip_check_items i WHERE i.trip_id = t.id)
		FROM trips t
		WHERE t.owner_id = ? OR t.share_family = 1
		   OR EXISTS (SELECT 1 FROM trip_members m WHERE m.trip_id = t.id AND m.user_id = ?)
		ORDER BY t.start_date DESC, t.id DESC`, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("list trips: %w", err)
	}
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		var s Summary
		var enc string
		if err := rows.Scan(&s.ID, &s.Kind, &s.StartDate, &s.EndDate, &s.DestTZ, &enc, &s.Open, &s.Total); err != nil {
			return nil, err
		}
		var d Doc
		if err := decryptJSON(enc, &d); err != nil {
			return nil, err
		}
		s.Title, s.Route = d.Title, d.Route
		for _, t := range d.Travellers {
			s.Travellers = append(s.Travellers, t.Name)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetTrip returns one trip with its checklists.
func GetTrip(ctx context.Context, db *sql.DB, tripID, userID int64) (*Trip, error) {
	canView, canEdit, err := access(ctx, db, tripID, userID)
	if err != nil {
		return nil, err
	}
	if !canView {
		return nil, ErrNotFound
	}
	var t Trip
	var enc string
	var race, result sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT id, owner_id, kind, start_date, end_date, home_tz, dest_tz, share_family,
		race_event_id, result_id, doc, created_at, updated_at FROM trips WHERE id = ?`, tripID).Scan(&t.ID, &t.OwnerID, &t.Kind,
		&t.StartDate, &t.EndDate, &t.HomeTZ, &t.DestTZ, &t.ShareFamily, &race, &result, &enc, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	if race.Valid {
		t.RaceEventID = &race.Int64
	}
	if result.Valid {
		t.ResultID = &result.Int64
	}
	if err := decryptJSON(enc, &t.Doc); err != nil {
		return nil, err
	}
	t.CanEdit = canEdit
	if t.Checklists, err = listChecklists(ctx, db, tripID); err != nil {
		return nil, err
	}
	return &t, nil
}

// CreateTrip adds a trip owned by the user.
func CreateTrip(ctx context.Context, db *sql.DB, userID int64, in TripInput) (int64, error) {
	if err := in.Normalize(); err != nil {
		return 0, err
	}
	enc, err := encryptJSON(in.Doc)
	if err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	ts := now()
	res, err := tx.ExecContext(ctx, `INSERT INTO trips (owner_id, kind, start_date, end_date, home_tz, dest_tz, share_family,
		race_event_id, result_id, doc, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, in.Kind, in.StartDate, in.EndDate, in.HomeTZ, in.DestTZ, in.ShareFamily, in.RaceEventID, in.ResultID, enc, ts, ts)
	if err != nil {
		return 0, fmt.Errorf("insert trip: %w", err)
	}
	id, _ := res.LastInsertId()
	if err := syncMembers(ctx, tx, id, in.Doc); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// UpdateTrip replaces a trip's fields and document.
func UpdateTrip(ctx context.Context, db *sql.DB, tripID, userID int64, in TripInput) error {
	_, canEdit, err := access(ctx, db, tripID, userID)
	if err != nil {
		return err
	}
	if !canEdit {
		return ErrForbidden
	}
	if err := in.Normalize(); err != nil {
		return err
	}
	enc, err := encryptJSON(in.Doc)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `UPDATE trips SET kind = ?, start_date = ?, end_date = ?, home_tz = ?, dest_tz = ?,
		share_family = ?, race_event_id = ?, result_id = ?, doc = ?, updated_at = ? WHERE id = ?`,
		in.Kind, in.StartDate, in.EndDate, in.HomeTZ, in.DestTZ, in.ShareFamily, in.RaceEventID, in.ResultID, enc, now(), tripID); err != nil {
		return fmt.Errorf("update trip: %w", err)
	}
	if err := syncMembers(ctx, tx, tripID, in.Doc); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteTrip removes a trip (owner only).
func DeleteTrip(ctx context.Context, db *sql.DB, tripID, userID int64) error {
	res, err := db.ExecContext(ctx, `DELETE FROM trips WHERE id = ? AND owner_id = ?`, tripID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- checklists -------------------------------------------------------------

func listChecklists(ctx context.Context, db *sql.DB, tripID int64) ([]Checklist, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, phase, title, position FROM trip_check_groups WHERE trip_id = ? ORDER BY position, id`, tripID)
	if err != nil {
		return nil, err
	}
	groups := []Checklist{}
	index := map[int64]int{}
	for rows.Next() {
		var g Checklist
		var title string
		if err := rows.Scan(&g.ID, &g.Phase, &title, &g.Position); err != nil {
			rows.Close()
			return nil, err
		}
		if err := decryptJSON(title, &g.Title); err != nil {
			rows.Close()
			return nil, err
		}
		g.Items = []CheckItem{}
		index[g.ID] = len(groups)
		groups = append(groups, g)
	}
	rows.Close()
	irows, err := db.QueryContext(ctx, `SELECT i.id, i.group_id, i.texts, i.urgent, i.done, COALESCE(u.name, i.done_by_name), i.done_at, i.position
		FROM trip_check_items i LEFT JOIN users u ON u.id = i.done_by WHERE i.trip_id = ? ORDER BY i.position, i.id`, tripID)
	if err != nil {
		return nil, err
	}
	defer irows.Close()
	for irows.Next() {
		var it CheckItem
		var gid int64
		var texts string
		if err := irows.Scan(&it.ID, &gid, &texts, &it.Urgent, &it.Done, &it.DoneByName, &it.DoneAt, &it.Position); err != nil {
			return nil, err
		}
		var tx struct {
			Title  I18n `json:"title"`
			Detail I18n `json:"detail"`
		}
		if err := decryptJSON(texts, &tx); err != nil {
			return nil, err
		}
		it.Title, it.Detail = tx.Title, tx.Detail
		if f := strings.Fields(it.DoneByName); len(f) > 0 {
			it.DoneByName = f[0]
		}
		if i, ok := index[gid]; ok {
			groups[i].Items = append(groups[i].Items, it)
		}
	}
	return groups, irows.Err()
}

func groupTrip(ctx context.Context, db *sql.DB, groupID int64) (int64, error) {
	var tripID int64
	err := db.QueryRowContext(ctx, `SELECT trip_id FROM trip_check_groups WHERE id = ?`, groupID).Scan(&tripID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return tripID, err
}

func itemTrip(ctx context.Context, db *sql.DB, itemID int64) (int64, error) {
	var tripID int64
	err := db.QueryRowContext(ctx, `SELECT trip_id FROM trip_check_items WHERE id = ?`, itemID).Scan(&tripID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return tripID, err
}

func requireEdit(ctx context.Context, db *sql.DB, tripID, userID int64) error {
	_, canEdit, err := access(ctx, db, tripID, userID)
	if err != nil {
		return err
	}
	if !canEdit {
		return ErrForbidden
	}
	return nil
}

// ChecklistInput creates or renames a checklist group.
type ChecklistInput struct {
	Phase string `json:"phase"`
	Title I18n   `json:"title"`
}

// AddChecklist adds a group at the end.
func AddChecklist(ctx context.Context, db *sql.DB, tripID, userID int64, in ChecklistInput) (int64, error) {
	if err := requireEdit(ctx, db, tripID, userID); err != nil {
		return 0, err
	}
	in.Title = trimI18n(in.Title)
	if len(in.Title) == 0 {
		return 0, invalid("checklist title is required")
	}
	enc, err := encryptJSON(in.Title)
	if err != nil {
		return 0, err
	}
	res, err := db.ExecContext(ctx, `INSERT INTO trip_check_groups (trip_id, phase, title, position)
		VALUES (?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM trip_check_groups WHERE trip_id = ?))`, tripID, in.Phase, enc, tripID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeleteChecklist removes a group and its items.
func DeleteChecklist(ctx context.Context, db *sql.DB, groupID, userID int64) error {
	tripID, err := groupTrip(ctx, db, groupID)
	if err != nil {
		return err
	}
	if err := requireEdit(ctx, db, tripID, userID); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM trip_check_groups WHERE id = ?`, groupID)
	return err
}

// ItemInput creates or edits a checklist item.
type ItemInput struct {
	Title  I18n `json:"title"`
	Detail I18n `json:"detail"`
	Urgent bool `json:"urgent"`
}

func (in *ItemInput) encrypt() (string, error) {
	in.Title, in.Detail = trimI18n(in.Title), trimI18n(in.Detail)
	if len(in.Title) == 0 {
		return "", invalid("item title is required")
	}
	return encryptJSON(struct {
		Title  I18n `json:"title"`
		Detail I18n `json:"detail"`
	}{in.Title, in.Detail})
}

// AddItem adds an item to a group.
func AddItem(ctx context.Context, db *sql.DB, groupID, userID int64, in ItemInput) (int64, error) {
	tripID, err := groupTrip(ctx, db, groupID)
	if err != nil {
		return 0, err
	}
	if err := requireEdit(ctx, db, tripID, userID); err != nil {
		return 0, err
	}
	enc, err := in.encrypt()
	if err != nil {
		return 0, err
	}
	res, err := db.ExecContext(ctx, `INSERT INTO trip_check_items (group_id, trip_id, texts, urgent, position)
		VALUES (?, ?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM trip_check_items WHERE group_id = ?))`, groupID, tripID, enc, in.Urgent, groupID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateItem edits an item's texts.
func UpdateItem(ctx context.Context, db *sql.DB, itemID, userID int64, in ItemInput) error {
	tripID, err := itemTrip(ctx, db, itemID)
	if err != nil {
		return err
	}
	if err := requireEdit(ctx, db, tripID, userID); err != nil {
		return err
	}
	enc, err := in.encrypt()
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE trip_check_items SET texts = ?, urgent = ? WHERE id = ?`, enc, in.Urgent, itemID)
	return err
}

// SetDone ticks or unticks an item, recording who did it.
func SetDone(ctx context.Context, db *sql.DB, itemID, userID int64, done bool) error {
	tripID, err := itemTrip(ctx, db, itemID)
	if err != nil {
		return err
	}
	if err := requireEdit(ctx, db, tripID, userID); err != nil {
		return err
	}
	if done {
		_, err = db.ExecContext(ctx, `UPDATE trip_check_items SET done = 1, done_by = ?, done_by_name = '', done_at = ? WHERE id = ?`, userID, now(), itemID)
	} else {
		_, err = db.ExecContext(ctx, `UPDATE trip_check_items SET done = 0, done_by = NULL, done_by_name = '', done_at = '' WHERE id = ?`, itemID)
	}
	return err
}

// DeleteItem removes an item.
func DeleteItem(ctx context.Context, db *sql.DB, itemID, userID int64) error {
	tripID, err := itemTrip(ctx, db, itemID)
	if err != nil {
		return err
	}
	if err := requireEdit(ctx, db, tripID, userID); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM trip_check_items WHERE id = ?`, itemID)
	return err
}
