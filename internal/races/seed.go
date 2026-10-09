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

// seedMigrationKey marks the seed as applied in schema_migrations, so races
// an admin deleted are not resurrected at the next restart.
const seedMigrationKey = "races_seed_2027"

type seedFile struct {
	CheckedAt string      `json:"checked_at"`
	Events    []seedEvent `json:"events"`
}

type seedEvent struct {
	Slug string `json:"slug"`
	EventInput
	Deadlines []DeadlineInput `json:"deadlines"`
}

// SeedCatalog loads the embedded starting catalog once. Races whose slug
// already exists are left alone.
func SeedCatalog(ctx context.Context, db *sql.DB) error {
	var done int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE key = ?`, seedMigrationKey).Scan(&done); err != nil {
		return fmt.Errorf("check race seed: %w", err)
	}
	if done > 0 {
		return nil
	}

	var seed seedFile
	if err := json.Unmarshal(seedJSON, &seed); err != nil {
		return fmt.Errorf("decode race seed: %w", err)
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
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (key, value) VALUES (?, '1')`, seedMigrationKey); err != nil {
		return fmt.Errorf("mark race seed: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("races: seeded %d catalog races", added)
	return nil
}
