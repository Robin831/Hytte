package races

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// ErrNotFound is returned when an event, deadline or watch does not exist.
var ErrNotFound = errors.New("not found")

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

const eventColumns = `id, slug, name, edition_year, race_date, date_precision, country, distance_m,
	status, entry_type, travel, url, series, texts, checked_at, created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanEvent(row scanner) (Event, error) {
	var e Event
	var series, texts string
	err := row.Scan(&e.ID, &e.Slug, &e.Name, &e.EditionYear, &e.RaceDate, &e.DatePrecision, &e.Country,
		&e.DistanceM, &e.Status, &e.EntryType, &e.Travel, &e.URL, &series, &texts,
		&e.CheckedAt, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return e, err
	}
	e.Series = splitSeries(series)
	e.Texts = map[string]EventText{}
	if err := json.Unmarshal([]byte(texts), &e.Texts); err != nil {
		return e, fmt.Errorf("decode texts for race %d: %w", e.ID, err)
	}
	e.Deadlines = []Deadline{}
	return e, nil
}

func splitSeries(s string) []string {
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

const deadlineColumns = `id, event_id, kind, due_date, date_precision, due_time, tz, expected, texts`

func scanDeadline(row scanner) (Deadline, error) {
	var d Deadline
	var texts string
	if err := row.Scan(&d.ID, &d.EventID, &d.Kind, &d.DueDate, &d.DatePrecision, &d.DueTime, &d.TZ,
		&d.Expected, &texts); err != nil {
		return d, err
	}
	d.Texts = map[string]DeadlineText{}
	if err := json.Unmarshal([]byte(texts), &d.Texts); err != nil {
		return d, fmt.Errorf("decode texts for deadline %d: %w", d.ID, err)
	}
	d.DueAt = dueAt(d.DueDate, d.DueTime, d.TZ)
	return d, nil
}

// ListEvents returns the whole catalog, sorted by race date, each with its
// deadlines in due order. The catalog is small (tens to a few hundred races),
// so filtering is left to the client.
func ListEvents(ctx context.Context, db *sql.DB) ([]Event, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+eventColumns+` FROM race_events ORDER BY race_date, name`)
	if err != nil {
		return nil, fmt.Errorf("list race events: %w", err)
	}
	defer rows.Close()
	events := []Event{}
	index := map[int64]int{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		index[e.ID] = len(events)
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	drows, err := db.QueryContext(ctx, `SELECT `+deadlineColumns+` FROM race_deadlines ORDER BY due_date, due_time, id`)
	if err != nil {
		return nil, fmt.Errorf("list race deadlines: %w", err)
	}
	defer drows.Close()
	for drows.Next() {
		d, err := scanDeadline(drows)
		if err != nil {
			return nil, err
		}
		if i, ok := index[d.EventID]; ok {
			events[i].Deadlines = append(events[i].Deadlines, d)
		}
	}
	return events, drows.Err()
}

// GetEvent returns one event with its deadlines.
func GetEvent(ctx context.Context, db *sql.DB, id int64) (*Event, error) {
	return getEvent(ctx, db, id)
}

func getEvent(ctx context.Context, q querier, id int64) (*Event, error) {
	e, err := scanEvent(q.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM race_events WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get race event %d: %w", id, err)
	}
	rows, err := q.QueryContext(ctx, `SELECT `+deadlineColumns+` FROM race_deadlines WHERE event_id = ?
		ORDER BY due_date, due_time, id`, id)
	if err != nil {
		return nil, fmt.Errorf("list deadlines for race %d: %w", id, err)
	}
	defer rows.Close()
	for rows.Next() {
		d, err := scanDeadline(rows)
		if err != nil {
			return nil, err
		}
		e.Deadlines = append(e.Deadlines, d)
	}
	return &e, rows.Err()
}

func marshalTexts(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode texts: %w", err)
	}
	return string(b), nil
}

// uniqueSlug derives a URL-safe slug from the name and year, suffixing a
// counter when another race already holds it.
func uniqueSlug(ctx context.Context, q querier, name string, year int) (string, error) {
	base := slugify(name)
	if base == "" {
		base = "race"
	}
	base += "-" + strconv.Itoa(year)
	slug := base
	for i := 2; ; i++ {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM race_events WHERE slug = ?`, slug).Scan(&n); err != nil {
			return "", fmt.Errorf("check slug: %w", err)
		}
		if n == 0 {
			return slug, nil
		}
		slug = base + "-" + strconv.Itoa(i)
	}
}

var slugReplacer = strings.NewReplacer("æ", "ae", "ø", "o", "å", "a", "ä", "a", "ö", "o", "ü", "u",
	"é", "e", "è", "e", "á", "a", "ó", "o", "í", "i", "ł", "l", "ß", "ss", "ç", "c", "ñ", "n")

func slugify(s string) string {
	s = slugReplacer.Replace(strings.ToLower(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

func nullableUser(userID int64) any {
	if userID == 0 {
		return nil
	}
	return userID
}

func logChange(ctx context.Context, q querier, eventID int64, field, oldV, newV, source string, userID int64) error {
	_, err := q.ExecContext(ctx, `INSERT INTO race_changes (event_id, field, old_value, new_value, source, user_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, eventID, field, oldV, newV, source, nullableUser(userID), now())
	if err != nil {
		return fmt.Errorf("log race change: %w", err)
	}
	return nil
}

// CreateEvent adds a race to the catalog. checkedAt stamps when its facts
// were last verified; empty means now.
func CreateEvent(ctx context.Context, db *sql.DB, in EventInput, checkedAt, source string, userID int64) (*Event, error) {
	if err := in.Normalize(); err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	slug, err := uniqueSlug(ctx, tx, in.Name, in.EditionYear)
	if err != nil {
		return nil, err
	}
	id, err := insertEvent(ctx, tx, slug, in, checkedAt)
	if err != nil {
		return nil, err
	}
	if err := logChange(ctx, tx, id, "created", "", in.Name, source, userID); err != nil {
		return nil, err
	}
	e, err := getEvent(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	return e, tx.Commit()
}

func insertEvent(ctx context.Context, q querier, slug string, in EventInput, checkedAt string) (int64, error) {
	texts, err := marshalTexts(in.Texts)
	if err != nil {
		return 0, err
	}
	ts := now()
	if checkedAt == "" {
		checkedAt = ts
	}
	res, err := q.ExecContext(ctx, `INSERT INTO race_events (slug, name, edition_year, race_date, date_precision,
		country, distance_m, status, entry_type, travel, url, series, texts, checked_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		slug, in.Name, in.EditionYear, in.RaceDate, in.DatePrecision, in.Country, in.DistanceM, in.Status,
		in.EntryType, in.Travel, in.URL, strings.Join(in.Series, ","), texts, checkedAt, ts, ts)
	if err != nil {
		return 0, fmt.Errorf("insert race event: %w", err)
	}
	return res.LastInsertId()
}

// fieldDiff is one changed field between the stored event and an edit.
type fieldDiff struct{ field, oldV, newV string }

func diffEvent(old *Event, in EventInput) []fieldDiff {
	var diffs []fieldDiff
	add := func(field, o, n string) {
		if o != n {
			diffs = append(diffs, fieldDiff{field, o, n})
		}
	}
	add("name", old.Name, in.Name)
	add("edition_year", strconv.Itoa(old.EditionYear), strconv.Itoa(in.EditionYear))
	add("race_date", old.RaceDate, in.RaceDate)
	add("date_precision", old.DatePrecision, in.DatePrecision)
	add("country", old.Country, in.Country)
	add("distance_m", strconv.Itoa(old.DistanceM), strconv.Itoa(in.DistanceM))
	add("status", old.Status, in.Status)
	add("entry_type", old.EntryType, in.EntryType)
	add("travel", old.Travel, in.Travel)
	add("url", old.URL, in.URL)
	add("series", strings.Join(old.Series, ","), strings.Join(in.Series, ","))
	for _, lang := range Languages {
		o, n := old.Texts[lang], in.Texts[lang]
		add("texts."+lang+".place", o.Place, n.Place)
		add("texts."+lang+".participants", o.Participants, n.Participants)
		add("texts."+lang+".course", o.Course, n.Course)
		add("texts."+lang+".travel", o.Travel, n.Travel)
		add("texts."+lang+".how", o.How, n.How)
		add("texts."+lang+".price", o.Price, n.Price)
	}
	return diffs
}

// UpdateEvent replaces an event's editable fields, logging one change row per
// field that actually changed, and stamps checked_at. It returns the updated
// event and the logged changes (empty when nothing differed).
func UpdateEvent(ctx context.Context, db *sql.DB, id int64, in EventInput, source string, userID int64) (*Event, []fieldDiff, error) {
	if err := in.Normalize(); err != nil {
		return nil, nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	old, err := getEvent(ctx, tx, id)
	if err != nil {
		return nil, nil, err
	}
	// Languages the edit didn't send keep their stored text, so an editor
	// working in one language never wipes the other two.
	for lang, t := range old.Texts {
		if _, sent := in.Texts[lang]; !sent {
			in.Texts[lang] = t
		}
	}
	diffs := diffEvent(old, in)
	texts, err := marshalTexts(in.Texts)
	if err != nil {
		return nil, nil, err
	}
	ts := now()
	if _, err := tx.ExecContext(ctx, `UPDATE race_events SET name = ?, edition_year = ?, race_date = ?,
		date_precision = ?, country = ?, distance_m = ?, status = ?, entry_type = ?, travel = ?, url = ?,
		series = ?, texts = ?, checked_at = ?, updated_at = ? WHERE id = ?`,
		in.Name, in.EditionYear, in.RaceDate, in.DatePrecision, in.Country, in.DistanceM, in.Status,
		in.EntryType, in.Travel, in.URL, strings.Join(in.Series, ","), texts, ts, ts, id); err != nil {
		return nil, nil, fmt.Errorf("update race event %d: %w", id, err)
	}
	for _, d := range diffs {
		if err := logChange(ctx, tx, id, d.field, d.oldV, d.newV, source, userID); err != nil {
			return nil, nil, err
		}
	}
	e, err := getEvent(ctx, tx, id)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	if old.RaceDate != in.RaceDate {
		syncStrideDates(ctx, db, id, old.RaceDate, in.RaceDate)
	}
	return e, diffs, nil
}

// DeleteEvent removes a race and (by cascade) its deadlines, changes and watches.
func DeleteEvent(ctx context.Context, db *sql.DB, id int64) error {
	res, err := db.ExecContext(ctx, `DELETE FROM race_events WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete race event %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// describeDeadline is how a deadline appears in the change log.
func describeDeadline(in DeadlineInput) string {
	s := in.DueDate
	if in.DueTime != "" {
		s += " " + in.DueTime + " " + in.TZ
	}
	if in.Expected {
		s += " (expected)"
	}
	return s
}

func insertDeadline(ctx context.Context, q querier, eventID int64, in DeadlineInput) (int64, error) {
	texts, err := marshalTexts(in.Texts)
	if err != nil {
		return 0, err
	}
	ts := now()
	res, err := q.ExecContext(ctx, `INSERT INTO race_deadlines (event_id, kind, due_date, date_precision, due_time, tz,
		expected, texts, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		eventID, in.Kind, in.DueDate, in.DatePrecision, in.DueTime, in.TZ, in.Expected, texts, ts, ts)
	if err != nil {
		return 0, fmt.Errorf("insert race deadline: %w", err)
	}
	return res.LastInsertId()
}

// CreateDeadline adds a deadline to an event and logs it.
func CreateDeadline(ctx context.Context, db *sql.DB, eventID int64, in DeadlineInput, source string, userID int64) (*Deadline, error) {
	if err := in.Normalize(); err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := getEvent(ctx, tx, eventID); err != nil {
		return nil, err
	}
	id, err := insertDeadline(ctx, tx, eventID, in)
	if err != nil {
		return nil, err
	}
	if err := logChange(ctx, tx, eventID, "deadline."+in.Kind, "", describeDeadline(in), source, userID); err != nil {
		return nil, err
	}
	if err := touchEvent(ctx, tx, eventID); err != nil {
		return nil, err
	}
	d, err := getDeadline(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	return d, tx.Commit()
}

func getDeadline(ctx context.Context, q querier, id int64) (*Deadline, error) {
	d, err := scanDeadline(q.QueryRowContext(ctx, `SELECT `+deadlineColumns+` FROM race_deadlines WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get race deadline %d: %w", id, err)
	}
	return &d, nil
}

func touchEvent(ctx context.Context, q querier, eventID int64) error {
	ts := now()
	_, err := q.ExecContext(ctx, `UPDATE race_events SET checked_at = ?, updated_at = ? WHERE id = ?`, ts, ts, eventID)
	return err
}

func deadlineAsInput(d *Deadline) DeadlineInput {
	return DeadlineInput{Kind: d.Kind, DueDate: d.DueDate, DatePrecision: d.DatePrecision, DueTime: d.DueTime,
		TZ: d.TZ, Expected: d.Expected, Texts: d.Texts}
}

// UpdateDeadline replaces a deadline's fields and logs the date change.
func UpdateDeadline(ctx context.Context, db *sql.DB, id int64, in DeadlineInput, source string, userID int64) (*Deadline, error) {
	if err := in.Normalize(); err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	old, err := getDeadline(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	for lang, t := range old.Texts {
		if _, sent := in.Texts[lang]; !sent {
			in.Texts[lang] = t
		}
	}
	texts, err := marshalTexts(in.Texts)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE race_deadlines SET kind = ?, due_date = ?, date_precision = ?,
		due_time = ?, tz = ?, expected = ?, texts = ?, updated_at = ? WHERE id = ?`,
		in.Kind, in.DueDate, in.DatePrecision, in.DueTime, in.TZ, in.Expected, texts, now(), id); err != nil {
		return nil, fmt.Errorf("update race deadline %d: %w", id, err)
	}
	oldDesc, newDesc := describeDeadline(deadlineAsInput(old)), describeDeadline(in)
	if oldDesc != newDesc || old.Kind != in.Kind {
		if err := logChange(ctx, tx, old.EventID, "deadline."+in.Kind, oldDesc, newDesc, source, userID); err != nil {
			return nil, err
		}
	}
	if err := touchEvent(ctx, tx, old.EventID); err != nil {
		return nil, err
	}
	d, err := getDeadline(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	return d, tx.Commit()
}

// DeleteDeadline removes a deadline and logs it.
func DeleteDeadline(ctx context.Context, db *sql.DB, id int64, source string, userID int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	old, err := getDeadline(ctx, tx, id)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM race_deadlines WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete race deadline %d: %w", id, err)
	}
	if err := logChange(ctx, tx, old.EventID, "deadline."+old.Kind, describeDeadline(deadlineAsInput(old)), "", source, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// ListChanges returns an event's change history, newest first.
func ListChanges(ctx context.Context, db *sql.DB, eventID int64, limit int) ([]Change, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, event_id, field, old_value, new_value, source, created_at
		FROM race_changes WHERE event_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, eventID, limit)
	if err != nil {
		return nil, fmt.Errorf("list race changes: %w", err)
	}
	defer rows.Close()
	changes := []Change{}
	for rows.Next() {
		var c Change
		if err := rows.Scan(&c.ID, &c.EventID, &c.Field, &c.OldValue, &c.NewValue, &c.Source, &c.CreatedAt); err != nil {
			return nil, err
		}
		changes = append(changes, c)
	}
	return changes, rows.Err()
}

func scanWatch(row scanner) (Watch, error) {
	var w Watch
	var notes string
	var stride sql.NullInt64
	if err := row.Scan(&w.EventID, &w.State, &notes, &stride, &w.CreatedAt, &w.UpdatedAt); err != nil {
		return w, err
	}
	w.Notes = encryption.DecryptLenient(notes)
	if stride.Valid {
		w.StrideRaceID = &stride.Int64
	}
	return w, nil
}

const watchColumns = `event_id, state, notes, stride_race_id, created_at, updated_at`

// ListWatches returns every race the user has a relationship with.
func ListWatches(ctx context.Context, db *sql.DB, userID int64) ([]Watch, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+watchColumns+` FROM race_watch WHERE user_id = ?
		ORDER BY created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("list race watches: %w", err)
	}
	defer rows.Close()
	watches := []Watch{}
	for rows.Next() {
		w, err := scanWatch(rows)
		if err != nil {
			return nil, err
		}
		watches = append(watches, w)
	}
	return watches, rows.Err()
}

// GetWatch returns the user's watch on one race, or ErrNotFound.
func GetWatch(ctx context.Context, db *sql.DB, userID, eventID int64) (*Watch, error) {
	w, err := scanWatch(db.QueryRowContext(ctx, `SELECT `+watchColumns+` FROM race_watch
		WHERE user_id = ? AND event_id = ?`, userID, eventID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get race watch: %w", err)
	}
	return &w, nil
}

// SetWatch creates or updates the user's watch on a race.
func SetWatch(ctx context.Context, db *sql.DB, userID, eventID int64, in WatchInput) (*Watch, error) {
	if err := in.Normalize(); err != nil {
		return nil, err
	}
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM race_events WHERE id = ?`, eventID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check race event: %w", err)
	}
	if exists == 0 {
		return nil, ErrNotFound
	}
	notes := ""
	if in.Notes != "" {
		enc, err := encryption.EncryptField(in.Notes)
		if err != nil {
			return nil, fmt.Errorf("encrypt race notes: %w", err)
		}
		notes = enc
	}
	var oldState string
	if err := db.QueryRowContext(ctx, `SELECT state FROM race_watch WHERE user_id = ? AND event_id = ?`, userID, eventID).Scan(&oldState); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("read race watch: %w", err)
	}
	ts := now()
	if _, err := db.ExecContext(ctx, `INSERT INTO race_watch (user_id, event_id, state, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, event_id) DO UPDATE SET state = excluded.state, notes = excluded.notes,
			updated_at = excluded.updated_at`, userID, eventID, in.State, notes, ts, ts); err != nil {
		return nil, fmt.Errorf("set race watch: %w", err)
	}
	recordWatchChange(ctx, db, userID, eventID, oldState, in.State)
	if in.State == "completed" && oldState != "completed" {
		autoFinish(ctx, db, userID, eventID)
	}
	return GetWatch(ctx, db, userID, eventID)
}

// DeleteWatch stops tracking a race for the user.
func DeleteWatch(ctx context.Context, db *sql.DB, userID, eventID int64) error {
	res, err := db.ExecContext(ctx, `DELETE FROM race_watch WHERE user_id = ? AND event_id = ?`, userID, eventID)
	if err != nil {
		return fmt.Errorf("delete race watch: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
