package trips

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// The family roster: everyone who travels, with or without a Hytte account.
// Trips pick their travellers from it, and birth years give the passenger
// mix (adults, children, infants) that flight and hotel prices depend on.
// Every Hytte user is on it automatically; people without an account (a
// spouse, a baby) are added by an admin. Names are encrypted at rest.

// Person is one member of the family roster.
type Person struct {
	ID        int64  `json:"id"`
	UserID    *int64 `json:"user_id"`
	Name      string `json:"name"`       // display name; an account's first name unless overridden
	BirthYear *int   `json:"birth_year"` // nil when unknown
}

// PersonInput is what an admin edits.
type PersonInput struct {
	Name      string `json:"name"`
	BirthYear *int   `json:"birth_year"`
}

func (in *PersonInput) normalize(requireName bool) error {
	in.Name = strings.TrimSpace(in.Name)
	switch {
	case requireName && in.Name == "":
		return invalid("name is required")
	case len([]rune(in.Name)) > 60:
		return invalid("name too long (max 60 characters)")
	case in.BirthYear != nil && (*in.BirthYear < 1900 || *in.BirthYear > time.Now().Year()):
		return invalid("birth year must be between 1900 and this year")
	}
	return nil
}

func firstName(full string) string {
	if f := strings.Fields(full); len(f) > 0 {
		return f[0]
	}
	return full
}

// ListPeople returns the roster: Hytte users first (each gets a roster row
// on first sight), then everyone else, by name.
func ListPeople(ctx context.Context, db *sql.DB) ([]Person, error) {
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO family_people (user_id, created_at, updated_at)
		SELECT id, ?, ? FROM users WHERE id > 0`, now(), now()); err != nil {
		return nil, fmt.Errorf("add users to roster: %w", err)
	}
	rows, err := db.QueryContext(ctx, `SELECT p.id, p.user_id, p.name, p.birth_year, COALESCE(u.name, '')
		FROM family_people p LEFT JOIN users u ON u.id = p.user_id
		ORDER BY p.user_id IS NULL, p.id`)
	if err != nil {
		return nil, fmt.Errorf("list roster: %w", err)
	}
	defer rows.Close()
	people := []Person{}
	for rows.Next() {
		var p Person
		var uid, by sql.NullInt64
		var enc, account string
		if err := rows.Scan(&p.ID, &uid, &enc, &by, &account); err != nil {
			return nil, err
		}
		if uid.Valid {
			p.UserID = &uid.Int64
		}
		if by.Valid {
			y := int(by.Int64)
			p.BirthYear = &y
		}
		if enc != "" {
			p.Name = encryption.DecryptLenient(enc)
		}
		if p.Name == "" {
			p.Name = firstName(account)
		}
		people = append(people, p)
	}
	return people, rows.Err()
}

func encryptName(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	return encryption.EncryptField(name)
}

// AddPerson adds someone without a Hytte account.
func AddPerson(ctx context.Context, db *sql.DB, in PersonInput) (int64, error) {
	if err := in.normalize(true); err != nil {
		return 0, err
	}
	enc, err := encryptName(in.Name)
	if err != nil {
		return 0, err
	}
	res, err := db.ExecContext(ctx, `INSERT INTO family_people (name, birth_year, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		enc, in.BirthYear, now(), now())
	if err != nil {
		return 0, fmt.Errorf("add person: %w", err)
	}
	return res.LastInsertId()
}

// UpdatePerson changes a name and birth year. For a Hytte user an empty name
// goes back to the account's first name.
func UpdatePerson(ctx context.Context, db *sql.DB, id int64, in PersonInput) error {
	var uid sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT user_id FROM family_people WHERE id = ?`, id).Scan(&uid); err == sql.ErrNoRows {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if err := in.normalize(!uid.Valid); err != nil {
		return err
	}
	enc, err := encryptName(in.Name)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE family_people SET name = ?, birth_year = ?, updated_at = ? WHERE id = ?`,
		enc, in.BirthYear, now(), id)
	return err
}

// DeletePerson removes someone without an account (users stay on the roster).
func DeletePerson(ctx context.Context, db *sql.DB, id int64) error {
	res, err := db.ExecContext(ctx, `DELETE FROM family_people WHERE id = ? AND user_id IS NULL`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ageOn is a person's age on a date, from their birth year alone (so it can
// be a year high before their birthday; good enough for fare classes).
func ageOn(birthYear int, date string) int {
	if len(date) < 4 {
		return 0
	}
	var y int
	fmt.Sscanf(date[:4], "%d", &y) //nolint:errcheck
	return y - birthYear
}

// resolveTravellers fills in roster travellers from the roster: name (when
// left empty), Hytte account and child flag (under 18 on the trip's first
// day). Unknown person IDs are an error.
func resolveTravellers(ctx context.Context, db *sql.DB, d *Doc, startDate string) error {
	var people map[int64]Person
	for i := range d.Travellers {
		t := &d.Travellers[i]
		if t.PersonID == nil {
			continue
		}
		if people == nil {
			list, err := ListPeople(ctx, db)
			if err != nil {
				return err
			}
			people = map[int64]Person{}
			for _, p := range list {
				people[p.ID] = p
			}
		}
		p, ok := people[*t.PersonID]
		if !ok {
			return invalid("unknown traveller %d", *t.PersonID)
		}
		if strings.TrimSpace(t.Name) == "" {
			t.Name = p.Name
		}
		t.UserID = p.UserID
		if p.BirthYear != nil {
			t.Child = ageOn(*p.BirthYear, startDate) < 18
		}
	}
	return nil
}
