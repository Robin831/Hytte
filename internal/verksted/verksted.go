// Package verksted is the Christmas presents planner. "Verksted" (the
// workshop) is a codename so the menu doesn't give it away: the household's
// kids are users too.
//
// Secrecy is the core rule: a user never sees gifts planned for a person
// linked to their own account, nor whether their own wishes are reserved.
package verksted

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/trips"
)

// Gift statuses, in order.
var Statuses = []string{"idea", "planned", "ordered", "arrived", "wrapped", "given"}

func validStatus(s string) bool {
	for _, v := range Statuses {
		if v == s {
			return true
		}
	}
	return false
}

// ErrNotFound is returned for missing (or hidden) rows.
var ErrNotFound = errors.New("not found")

// ErrInvalid wraps validation errors.
var ErrInvalid = errors.New("invalid")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func now() time.Time { return time.Now().UTC() }

func enc(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	return encryption.EncryptField(s)
}

func dec(s string) string {
	if s == "" {
		return ""
	}
	return encryption.DecryptLenient(s)
}

// CurrentSeason is the Christmas being planned: this year, except in
// January, which still belongs to last Christmas.
func CurrentSeason(t time.Time) int {
	if t.Month() == time.January {
		return t.Year() - 1
	}
	return t.Year()
}

// --- people --------------------------------------------------------------

// Person is someone to give to.
type Person struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Group     string  `json:"group"`
	UserID    *int64  `json:"user_id"`
	BirthYear *int    `json:"birth_year"`
	Sizes     string  `json:"sizes"`
	Notes     string  `json:"notes"`
	Archived  bool    `json:"archived"`
	BudgetNOK float64 `json:"budget_nok"` // for the season asked for
}

// PersonInput creates or updates a person; nil fields are left alone.
type PersonInput struct {
	Name      *string `json:"name"`
	Group     *string `json:"group"`
	BirthYear *int    `json:"birth_year"`
	Sizes     *string `json:"sizes"`
	Notes     *string `json:"notes"`
	Archived  *bool   `json:"archived"`
}

const familyGroup = "Familie"

// seedPeople adds the household roster (trips' family_people) once, so the
// family is there from the start. Later roster changes don't flow in.
func seedPeople(ctx context.Context, db *sql.DB) error {
	var done int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE key = 'verksted_seed_people'`).Scan(&done); err != nil || done > 0 {
		return err
	}
	roster, err := trips.ListPeople(ctx, db)
	if err != nil {
		return err
	}
	ts := now()
	for _, p := range roster {
		name, err := enc(p.Name)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO verksted_people (name, grp, user_id, roster_id, birth_year, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, name, familyGroup, p.UserID, p.ID, p.BirthYear, ts, ts); err != nil {
			return err
		}
	}
	_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (key, value) VALUES ('verksted_seed_people', '1')`)
	return err
}

func listPeople(ctx context.Context, db *sql.DB, season int) ([]Person, error) {
	rows, err := db.QueryContext(ctx, `SELECT p.id, p.name, p.grp, p.user_id, p.birth_year, p.sizes, p.notes, p.archived,
			COALESCE(b.budget_nok, 0)
		FROM verksted_people p
		LEFT JOIN verksted_person_budgets b ON b.person_id = p.id AND b.season = ?
		ORDER BY p.archived, p.grp = ? DESC, p.grp, p.birth_year IS NULL, p.birth_year DESC, p.id`, season, familyGroup)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Person{}
	for rows.Next() {
		var p Person
		var uid, by sql.NullInt64
		if err := rows.Scan(&p.ID, &p.Name, &p.Group, &uid, &by, &p.Sizes, &p.Notes, &p.Archived, &p.BudgetNOK); err != nil {
			return nil, err
		}
		p.Name, p.Sizes, p.Notes = dec(p.Name), dec(p.Sizes), dec(p.Notes)
		if uid.Valid {
			p.UserID = &uid.Int64
		}
		if by.Valid {
			y := int(by.Int64)
			p.BirthYear = &y
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func trimPtr(s *string, max int, field string) (*string, error) {
	if s == nil {
		return nil, nil
	}
	v := strings.TrimSpace(*s)
	if len(v) > max {
		return nil, invalid("%s is too long", field)
	}
	return &v, nil
}

// AddPerson adds someone to give to.
func AddPerson(ctx context.Context, db *sql.DB, in PersonInput) (int64, error) {
	name, err := trimPtr(in.Name, 100, "name")
	if err != nil {
		return 0, err
	}
	if name == nil || *name == "" {
		return 0, invalid("name is required")
	}
	id, err := insertPerson(ctx, db, *name)
	if err != nil {
		return 0, err
	}
	in.Name = nil
	return id, UpdatePerson(ctx, db, id, in)
}

func insertPerson(ctx context.Context, db *sql.DB, name string) (int64, error) {
	encName, err := enc(name)
	if err != nil {
		return 0, err
	}
	ts := now()
	res, err := db.ExecContext(ctx, `INSERT INTO verksted_people (name, created_at, updated_at) VALUES (?, ?, ?)`, encName, ts, ts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdatePerson changes the given fields.
func UpdatePerson(ctx context.Context, db *sql.DB, id int64, in PersonInput) error {
	sets := []string{}
	args := []any{}
	for _, f := range []struct {
		col   string
		val   *string
		max   int
		crypt bool
	}{
		{"name", in.Name, 100, true}, {"grp", in.Group, 50, false}, {"sizes", in.Sizes, 1000, true}, {"notes", in.Notes, 4000, true},
	} {
		v, err := trimPtr(f.val, f.max, f.col)
		if err != nil {
			return err
		}
		if v == nil {
			continue
		}
		if f.col == "name" && *v == "" {
			return invalid("name is required")
		}
		val := *v
		if f.crypt {
			if val, err = enc(val); err != nil {
				return err
			}
		}
		sets = append(sets, f.col+" = ?")
		args = append(args, val)
	}
	if in.BirthYear != nil {
		if *in.BirthYear != 0 && (*in.BirthYear < 1900 || *in.BirthYear > 2100) {
			return invalid("birth year must be between 1900 and 2100")
		}
		sets = append(sets, "birth_year = ?")
		if *in.BirthYear == 0 {
			args = append(args, nil)
		} else {
			args = append(args, *in.BirthYear)
		}
	}
	if in.Archived != nil {
		sets = append(sets, "archived = ?")
		args = append(args, *in.Archived)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, now(), id)
	res, err := db.ExecContext(ctx, `UPDATE verksted_people SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePerson removes a person with their wishes; gifts lose them as a
// recipient (and gifts left with no recipient go too).
func DeletePerson(ctx context.Context, db *sql.DB, id int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	res, err := tx.ExecContext(ctx, `DELETE FROM verksted_people WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM verksted_gifts WHERE id NOT IN (SELECT gift_id FROM verksted_gift_recipients)`); err != nil {
		return err
	}
	return tx.Commit()
}

// SetPersonBudget sets a person's budget for a season (0 clears it).
func SetPersonBudget(ctx context.Context, db *sql.DB, personID int64, season int, nok float64) error {
	if nok < 0 || nok > 10_000_000 {
		return invalid("budget must be between 0 and 10 000 000")
	}
	if err := checkSeason(season); err != nil {
		return err
	}
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM verksted_people WHERE id = ?`, personID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrNotFound
	}
	_, err := db.ExecContext(ctx, `INSERT INTO verksted_person_budgets (person_id, season, budget_nok) VALUES (?, ?, ?)
		ON CONFLICT(person_id, season) DO UPDATE SET budget_nok = excluded.budget_nok`, personID, season, nok)
	return err
}

// SetTotalBudget sets the season's total budget.
func SetTotalBudget(ctx context.Context, db *sql.DB, season int, nok float64) error {
	if nok < 0 || nok > 10_000_000 {
		return invalid("budget must be between 0 and 10 000 000")
	}
	if err := checkSeason(season); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO verksted_budgets (season, total_nok) VALUES (?, ?)
		ON CONFLICT(season) DO UPDATE SET total_nok = excluded.total_nok`, season, nok)
	return err
}

func checkSeason(season int) error {
	if season < 2000 || season > 2100 {
		return invalid("season must be a year")
	}
	return nil
}

// --- viewer & secrecy -----------------------------------------------------

// ownPeople returns the people linked to the viewer's account: gifts for
// them are hidden from the viewer.
func ownPeople(ctx context.Context, db *sql.DB, userID int64) (map[int64]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT id FROM verksted_people WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// --- wishes ----------------------------------------------------------------

// Wish is something a person wishes for.
type Wish struct {
	ID         int64   `json:"id"`
	PersonID   int64   `json:"person_id"`
	Title      string  `json:"title"`
	URL        string  `json:"url"`
	Notes      string  `json:"notes"`
	PriceNOK   float64 `json:"price_nok"`
	Priority   int     `json:"priority"`
	Reserved   bool    `json:"reserved"`
	ReservedBy *int64  `json:"reserved_by"`
	// GiftID links the gift made from this wish, when visible.
	GiftID *int64 `json:"gift_id"`
}

// WishInput creates or updates a wish.
type WishInput struct {
	PersonID *int64   `json:"person_id"`
	Title    *string  `json:"title"`
	URL      *string  `json:"url"`
	Notes    *string  `json:"notes"`
	PriceNOK *float64 `json:"price_nok"`
	Priority *int     `json:"priority"`
}

func listWishes(ctx context.Context, db *sql.DB, own map[int64]bool) ([]Wish, error) {
	rows, err := db.QueryContext(ctx, `SELECT w.id, w.person_id, w.title, w.url, w.notes, w.price_nok, w.priority, w.reserved_by
		FROM verksted_wishes w JOIN verksted_people p ON p.id = w.person_id
		ORDER BY w.person_id, w.priority DESC, w.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Wish{}
	for rows.Next() {
		var w Wish
		var by sql.NullInt64
		if err := rows.Scan(&w.ID, &w.PersonID, &w.Title, &w.URL, &w.Notes, &w.PriceNOK, &w.Priority, &by); err != nil {
			return nil, err
		}
		w.Title, w.URL, w.Notes = dec(w.Title), dec(w.URL), dec(w.Notes)
		// The wisher doesn't learn whether their wish is taken.
		if by.Valid && !own[w.PersonID] {
			w.Reserved = true
			w.ReservedBy = &by.Int64
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// AddWish adds a wish for a person.
func AddWish(ctx context.Context, db *sql.DB, userID int64, in WishInput) (int64, error) {
	if in.PersonID == nil {
		return 0, invalid("person_id is required")
	}
	if in.Title == nil || strings.TrimSpace(*in.Title) == "" {
		return 0, invalid("title is required")
	}
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM verksted_people WHERE id = ?`, *in.PersonID).Scan(&exists); err != nil {
		return 0, err
	}
	if exists == 0 {
		return 0, invalid("unknown person")
	}
	title, err := enc(strings.TrimSpace(*in.Title))
	if err != nil {
		return 0, err
	}
	ts := now()
	res, err := db.ExecContext(ctx, `INSERT INTO verksted_wishes (person_id, title, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		*in.PersonID, title, userID, ts, ts)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	in.PersonID, in.Title = nil, nil
	return id, UpdateWish(ctx, db, id, in)
}

// UpdateWish changes the given fields.
func UpdateWish(ctx context.Context, db *sql.DB, id int64, in WishInput) error {
	sets := []string{}
	args := []any{}
	for _, f := range []struct {
		col string
		val *string
		max int
	}{{"title", in.Title, 300}, {"url", in.URL, 2000}, {"notes", in.Notes, 4000}} {
		v, err := trimPtr(f.val, f.max, f.col)
		if err != nil {
			return err
		}
		if v == nil {
			continue
		}
		if f.col == "title" && *v == "" {
			return invalid("title is required")
		}
		e, err := enc(*v)
		if err != nil {
			return err
		}
		sets = append(sets, f.col+" = ?")
		args = append(args, e)
	}
	if in.PriceNOK != nil {
		if *in.PriceNOK < 0 || *in.PriceNOK > 10_000_000 {
			return invalid("price must be between 0 and 10 000 000")
		}
		sets = append(sets, "price_nok = ?")
		args = append(args, *in.PriceNOK)
	}
	if in.Priority != nil {
		if *in.Priority < 1 || *in.Priority > 3 {
			return invalid("priority must be 1–3")
		}
		sets = append(sets, "priority = ?")
		args = append(args, *in.Priority)
	}
	if in.PersonID != nil {
		sets = append(sets, "person_id = ?")
		args = append(args, *in.PersonID)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, now(), id)
	res, err := db.ExecContext(ctx, `UPDATE verksted_wishes SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteWish removes a wish.
func DeleteWish(ctx context.Context, db *sql.DB, id int64) error {
	res, err := db.ExecContext(ctx, `DELETE FROM verksted_wishes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ReserveWish marks (or unmarks) a wish as being bought by the user. The
// wisher can't reserve their own wish — it would tell them nothing useful.
func ReserveWish(ctx context.Context, db *sql.DB, userID, id int64, reserve bool) error {
	own, err := ownPeople(ctx, db, userID)
	if err != nil {
		return err
	}
	var personID int64
	if err := db.QueryRowContext(ctx, `SELECT person_id FROM verksted_wishes WHERE id = ?`, id).Scan(&personID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if own[personID] {
		return invalid("you can't reserve your own wish")
	}
	var by any
	if reserve {
		by = userID
	}
	_, err = db.ExecContext(ctx, `UPDATE verksted_wishes SET reserved_by = ?, updated_at = ? WHERE id = ?`, by, now(), id)
	return err
}

// --- gifts -------------------------------------------------------------------

// Gift is a present planned for one or more people.
type Gift struct {
	ID           int64   `json:"id"`
	Season       int     `json:"season"`
	Title        string  `json:"title"`
	Notes        string  `json:"notes"`
	URL          string  `json:"url"`
	Shop         string  `json:"shop"`
	Giver        string  `json:"giver"`
	PriceNOK     float64 `json:"price_nok"`
	Status       string  `json:"status"`
	WishID       *int64  `json:"wish_id"`
	RecipientIDs []int64 `json:"recipient_ids"`
}

// GiftInput creates or updates a gift.
type GiftInput struct {
	Season       *int     `json:"season"`
	Title        *string  `json:"title"`
	Notes        *string  `json:"notes"`
	URL          *string  `json:"url"`
	Shop         *string  `json:"shop"`
	Giver        *string  `json:"giver"`
	PriceNOK     *float64 `json:"price_nok"`
	Status       *string  `json:"status"`
	WishID       *int64   `json:"wish_id"`
	RecipientIDs *[]int64 `json:"recipient_ids"`
}

func listGifts(ctx context.Context, db *sql.DB, season int, own map[int64]bool) ([]Gift, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, season, title, notes, url, shop, giver, price_nok, status, wish_id
		FROM verksted_gifts WHERE season = ? ORDER BY id`, season)
	if err != nil {
		return nil, err
	}
	var gifts []Gift
	index := map[int64]int{}
	for rows.Next() {
		var g Gift
		var wish sql.NullInt64
		if err := rows.Scan(&g.ID, &g.Season, &g.Title, &g.Notes, &g.URL, &g.Shop, &g.Giver, &g.PriceNOK, &g.Status, &wish); err != nil {
			rows.Close()
			return nil, err
		}
		g.Title, g.Notes, g.URL, g.Shop, g.Giver = dec(g.Title), dec(g.Notes), dec(g.URL), dec(g.Shop), dec(g.Giver)
		if wish.Valid {
			g.WishID = &wish.Int64
		}
		g.RecipientIDs = []int64{}
		index[g.ID] = len(gifts)
		gifts = append(gifts, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = db.QueryContext(ctx, `SELECT r.gift_id, r.person_id FROM verksted_gift_recipients r
		JOIN verksted_gifts g ON g.id = r.gift_id WHERE g.season = ? ORDER BY r.person_id`, season)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var gid, pid int64
		if err := rows.Scan(&gid, &pid); err != nil {
			rows.Close()
			return nil, err
		}
		if i, ok := index[gid]; ok {
			gifts[i].RecipientIDs = append(gifts[i].RecipientIDs, pid)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []Gift{}
	for _, g := range gifts {
		if forOwn(g, own) {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

// forOwn reports a gift for someone linked to the viewer.
func forOwn(g Gift, own map[int64]bool) bool {
	for _, pid := range g.RecipientIDs {
		if own[pid] {
			return true
		}
	}
	return false
}

// visibleGift loads a gift, or ErrNotFound when it doesn't exist or is for
// the viewer (so they can't edit, or learn about, their own presents).
func visibleGift(ctx context.Context, db *sql.DB, userID, id int64) (*Gift, error) {
	var season int
	if err := db.QueryRowContext(ctx, `SELECT season FROM verksted_gifts WHERE id = ?`, id).Scan(&season); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	own, err := ownPeople(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	gifts, err := listGifts(ctx, db, season, own)
	if err != nil {
		return nil, err
	}
	for i := range gifts {
		if gifts[i].ID == id {
			return &gifts[i], nil
		}
	}
	return nil, ErrNotFound
}

// AddGift creates a gift. A gift made from a wish reserves the wish.
func AddGift(ctx context.Context, db *sql.DB, userID int64, in GiftInput) (int64, error) {
	if in.Title == nil || strings.TrimSpace(*in.Title) == "" {
		return 0, invalid("title is required")
	}
	if in.RecipientIDs == nil || len(*in.RecipientIDs) == 0 {
		return 0, invalid("pick at least one recipient")
	}
	season := CurrentSeason(time.Now())
	if in.Season != nil {
		season = *in.Season
	}
	if err := checkSeason(season); err != nil {
		return 0, err
	}
	if err := checkRecipients(ctx, db, userID, *in.RecipientIDs); err != nil {
		return 0, err
	}
	title, err := enc(strings.TrimSpace(*in.Title))
	if err != nil {
		return 0, err
	}
	ts := now()
	res, err := db.ExecContext(ctx, `INSERT INTO verksted_gifts (season, title, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		season, title, userID, ts, ts)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	in.Title, in.Season = nil, nil
	if err := updateGift(ctx, db, userID, id, in); err != nil {
		return 0, err
	}
	if in.WishID != nil {
		if err := ReserveWish(ctx, db, userID, *in.WishID, true); err != nil && !errors.Is(err, ErrInvalid) {
			return 0, err
		}
	}
	return id, nil
}

// checkRecipients refuses unknown people and the viewer's own people (a
// gift for yourself would vanish from your own view).
func checkRecipients(ctx context.Context, db *sql.DB, userID int64, ids []int64) error {
	own, err := ownPeople(ctx, db, userID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		var exists int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM verksted_people WHERE id = ?`, id).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return invalid("unknown recipient")
		}
		if own[id] {
			return invalid("you can't plan a gift for yourself here")
		}
	}
	return nil
}

// UpdateGift changes a visible gift.
func UpdateGift(ctx context.Context, db *sql.DB, userID, id int64, in GiftInput) error {
	if _, err := visibleGift(ctx, db, userID, id); err != nil {
		return err
	}
	return updateGift(ctx, db, userID, id, in)
}

func updateGift(ctx context.Context, db *sql.DB, userID, id int64, in GiftInput) error {
	sets := []string{}
	args := []any{}
	for _, f := range []struct {
		col string
		val *string
		max int
	}{{"title", in.Title, 300}, {"notes", in.Notes, 4000}, {"url", in.URL, 2000}, {"shop", in.Shop, 200}, {"giver", in.Giver, 200}} {
		v, err := trimPtr(f.val, f.max, f.col)
		if err != nil {
			return err
		}
		if v == nil {
			continue
		}
		if f.col == "title" && *v == "" {
			return invalid("title is required")
		}
		e, err := enc(*v)
		if err != nil {
			return err
		}
		sets = append(sets, f.col+" = ?")
		args = append(args, e)
	}
	if in.PriceNOK != nil {
		if *in.PriceNOK < 0 || *in.PriceNOK > 10_000_000 {
			return invalid("price must be between 0 and 10 000 000")
		}
		sets = append(sets, "price_nok = ?")
		args = append(args, *in.PriceNOK)
	}
	if in.Status != nil {
		if !validStatus(*in.Status) {
			return invalid("unknown status %q", *in.Status)
		}
		sets = append(sets, "status = ?")
		args = append(args, *in.Status)
	}
	if in.Season != nil {
		if err := checkSeason(*in.Season); err != nil {
			return err
		}
		sets = append(sets, "season = ?")
		args = append(args, *in.Season)
	}
	if in.WishID != nil {
		sets = append(sets, "wish_id = ?")
		if *in.WishID == 0 {
			args = append(args, nil)
		} else {
			args = append(args, *in.WishID)
		}
	}
	// Validate before opening the transaction: checks use their own
	// connection, which would wait on the transaction's one.
	if in.RecipientIDs != nil {
		if len(*in.RecipientIDs) == 0 {
			return invalid("pick at least one recipient")
		}
		if err := checkRecipients(ctx, db, userID, *in.RecipientIDs); err != nil {
			return err
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if len(sets) > 0 {
		sets = append(sets, "updated_at = ?")
		args = append(args, now(), id)
		if _, err := tx.ExecContext(ctx, `UPDATE verksted_gifts SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
			return err
		}
	}
	if in.RecipientIDs != nil {
		ids := *in.RecipientIDs
		if _, err := tx.ExecContext(ctx, `DELETE FROM verksted_gift_recipients WHERE gift_id = ?`, id); err != nil {
			return err
		}
		for _, pid := range ids {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO verksted_gift_recipients (gift_id, person_id) VALUES (?, ?)`, id, pid); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// DeleteGift removes a visible gift.
func DeleteGift(ctx context.Context, db *sql.DB, userID, id int64) error {
	if _, err := visibleGift(ctx, db, userID, id); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `DELETE FROM verksted_gifts WHERE id = ?`, id)
	return err
}

// --- overview ---------------------------------------------------------------

// Overview is everything the page shows for a season.
type Overview struct {
	Season         int      `json:"season"`
	Seasons        []int    `json:"seasons"`
	TotalBudgetNOK float64  `json:"total_budget_nok"`
	People         []Person `json:"people"`
	Wishes         []Wish   `json:"wishes"`
	Gifts          []Gift   `json:"gifts"`
}

// LoadOverview loads a season as the viewer may see it.
func LoadOverview(ctx context.Context, db *sql.DB, userID int64, season int) (*Overview, error) {
	if err := seedPeople(ctx, db); err != nil {
		return nil, fmt.Errorf("seed people: %w", err)
	}
	own, err := ownPeople(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	o := &Overview{Season: season}
	if err := db.QueryRowContext(ctx, `SELECT COALESCE((SELECT total_nok FROM verksted_budgets WHERE season = ?), 0)`, season).Scan(&o.TotalBudgetNOK); err != nil {
		return nil, err
	}
	if o.People, err = listPeople(ctx, db, season); err != nil {
		return nil, err
	}
	if o.Wishes, err = listWishes(ctx, db, own); err != nil {
		return nil, err
	}
	if o.Gifts, err = listGifts(ctx, db, season, own); err != nil {
		return nil, err
	}
	// Link wishes to the (visible) gifts made from them.
	byWish := map[int64]int64{}
	for _, g := range o.Gifts {
		if g.WishID != nil {
			byWish[*g.WishID] = g.ID
		}
	}
	for i := range o.Wishes {
		if gid, ok := byWish[o.Wishes[i].ID]; ok {
			gid := gid
			o.Wishes[i].GiftID = &gid
		}
	}
	seasons := map[int]bool{season: true, CurrentSeason(time.Now()): true}
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT season FROM verksted_gifts UNION SELECT season FROM verksted_budgets`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s int
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return nil, err
		}
		seasons[s] = true
	}
	rows.Close()
	for s := range seasons {
		o.Seasons = append(o.Seasons, s)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(o.Seasons)))
	return o, rows.Err()
}
