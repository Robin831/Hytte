package races

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
)

// seedJSON is the starting catalog: European half marathons, the World
// Marathon Majors and the European Marathon Classics for 2027, researched in
// October 2026, with texts in every catalog language.
//
//go:embed seed.json
var seedJSON []byte

// seed2027bJSON adds the big European full marathons, full distances of
// races whose half was already listed, Norwegian races and Thai races.
//
//go:embed seed_2027b.json
var seed2027bJSON []byte

// seedSources are applied in order, each once. The key marks a source as
// applied in schema_migrations, so races an admin deleted are not
// resurrected at the next restart; a new batch gets a new key.
var seedSources = []struct {
	key  string
	data []byte
}{
	{"races_seed_2027", seedJSON},
	{"races_seed_2027_b", seed2027bJSON},
}

type seedFile struct {
	CheckedAt string      `json:"checked_at"`
	Events    []seedEvent `json:"events"`
}

type seedEvent struct {
	Slug string `json:"slug"`
	EventInput
	Deadlines []DeadlineInput `json:"deadlines"`
}

// catalogFixes correct seeded races that are already live. Each runs once
// (keyed in schema_migrations) through UpdateEvent, so the correction shows
// in the race's change history and reaches watchers like any other edit. A
// race that was deleted or no longer matches is skipped.
var catalogFixes = []struct {
	key  string
	slug string
	fix  func(in *EventInput)
}{
	{
		// The Amsterdam races run on the third Sunday of October; the seed had
		// a Monday. Same day as the full marathon added in batch b.
		key:  "races_fix_2027_amsterdam_half_date",
		slug: "tcs-amsterdam-marathon-halvmaraton-2027",
		fix: func(in *EventInput) {
			if in.RaceDate == "2027-10-18" {
				in.RaceDate = "2027-10-17"
				in.DatePrecision = "approx"
			}
		},
	},
	{
		// Trondheim Maraton is 4–5 Sep 2027; since 2026 the Saturday is the
		// children's day and the main races run on the Sunday.
		key:  "races_fix_2027_trondheim_half_date",
		slug: "trondheim-maraton-halvmaraton-2027",
		fix: func(in *EventInput) {
			if in.RaceDate == "2027-09-04" {
				in.RaceDate = "2027-09-05"
				in.DatePrecision = "approx"
			}
		},
	},
}

// SeedCatalog loads every embedded seed batch that hasn't been applied yet,
// then applies pending catalog fixes. Races whose slug already exists are
// left alone by the seeds.
func SeedCatalog(ctx context.Context, db *sql.DB) error {
	for _, src := range seedSources {
		if err := applySeed(ctx, db, src.key, src.data); err != nil {
			return err
		}
	}
	for _, f := range catalogFixes {
		if err := applyFix(ctx, db, f.key, f.slug, f.fix); err != nil {
			return err
		}
	}
	return backfillWatchHistory(ctx, db)
}

func applyFix(ctx context.Context, db *sql.DB, key, slug string, fix func(*EventInput)) error {
	var done int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE key = ?`, key).Scan(&done); err != nil {
		return fmt.Errorf("check race fix %s: %w", key, err)
	}
	if done > 0 {
		return nil
	}
	var id int64
	err := db.QueryRowContext(ctx, `SELECT id FROM race_events WHERE slug = ?`, slug).Scan(&id)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("race fix %s: %w", key, err)
	}
	if err == nil {
		e, err := GetEvent(ctx, db, id)
		if err != nil {
			return fmt.Errorf("race fix %s: %w", key, err)
		}
		in := EventInput{Name: e.Name, EditionYear: e.EditionYear, RaceDate: e.RaceDate, DatePrecision: e.DatePrecision,
			Country: e.Country, DistanceM: e.DistanceM, Status: e.Status, EntryType: e.EntryType, Travel: e.Travel,
			URL: e.URL, Series: e.Series, Texts: e.Texts}
		fix(&in)
		if _, _, err := UpdateEvent(ctx, db, id, in, "seed", 0); err != nil {
			return fmt.Errorf("race fix %s: %w", key, err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (key, value) VALUES (?, '1')`, key); err != nil {
		return fmt.Errorf("mark race fix %s: %w", key, err)
	}
	return nil
}

func applySeed(ctx context.Context, db *sql.DB, key string, data []byte) error {
	var done int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE key = ?`, key).Scan(&done); err != nil {
		return fmt.Errorf("check race seed %s: %w", key, err)
	}
	if done > 0 {
		return nil
	}

	var seed seedFile
	if err := json.Unmarshal(data, &seed); err != nil {
		return fmt.Errorf("decode race seed %s: %w", key, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	added := 0
	for _, se := range seed.Events {
		in := se.EventInput
		if err := in.Normalize(); err != nil {
			return fmt.Errorf("race seed %s: %w", se.Slug, err)
		}
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM race_events WHERE slug = ?`, se.Slug).Scan(&exists); err != nil {
			return fmt.Errorf("check race seed %s: %w", se.Slug, err)
		}
		if exists > 0 {
			continue
		}
		id, err := insertEvent(ctx, tx, se.Slug, in, seed.CheckedAt)
		if err != nil {
			return fmt.Errorf("race seed %s: %w", se.Slug, err)
		}
		for _, d := range se.Deadlines {
			if err := d.Normalize(); err != nil {
				return fmt.Errorf("race seed %s deadline: %w", se.Slug, err)
			}
			if _, err := insertDeadline(ctx, tx, id, d); err != nil {
				return fmt.Errorf("race seed %s deadline: %w", se.Slug, err)
			}
		}
		added++
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (key, value) VALUES (?, '1')`, key); err != nil {
		return fmt.Errorf("mark race seed %s: %w", key, err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("races: seed %s added %d catalog races", key, added)
	return nil
}
