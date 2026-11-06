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

// seed2027cJSON adds the two SuperHalfs Europa races (announced 6 Nov 2026)
// that weren't in the catalog yet: Napoli and Belgrade.
//
//go:embed seed_2027c.json
var seed2027cJSON []byte

// seedSources are applied in order, each once. The key marks a source as
// applied in schema_migrations, so races an admin deleted are not
// resurrected at the next restart; a new batch gets a new key.
var seedSources = []struct {
	key  string
	data []byte
}{
	{"races_seed_2027", seedJSON},
	{"races_seed_2027_b", seed2027bJSON},
	{"races_seed_2027_c", seed2027cJSON},
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
	{
		// The Warsaw half moved from 4 April to 14 March 2027 (organiser and
		// superhalfs.com, Nov 2026); entry opens 16 Nov 2026.
		key:  "races_fix_2027_warsaw_half_date",
		slug: "nn-polmaraton-warszawski-2027",
		fix: func(in *EventInput) {
			if in.RaceDate != "2027-04-04" {
				return
			}
			in.RaceDate = "2027-03-14"
			in.DatePrecision = "day"
			in.URL = "https://polmaratonwarszawski.com/en/"
			setText(in, "nb", "Ca. 22 500 fullførte 20. utgave i mars 2026 (over 30 000 med 5 km). 2026 ble utsolgt; taket for 2027 er 28 000.",
				"Datoen er flyttet fra 4. april til 14. mars 2027. Påmeldingen åpner 16. november 2026 på rejestracja.maratonwarszawski.com, trolig etter førstemann-til-mølla med prisøkninger underveis. Del av SuperHalfs Europa fra 2027.")
			setText(in, "en", "Approx. 22,500 finished the 20th edition in March 2026 (over 30,000 including the 5 km). 2026 sold out; the 2027 cap is 28,000.",
				"The date moved from 4 April to 14 March 2027. Entry opens on 16 November 2026 at rejestracja.maratonwarszawski.com, probably first come, first served with rising prices. Part of SuperHalfs Europa from 2027.")
			setText(in, "th", "ประมาณ 22,500 คนวิ่งจบครั้งที่ 20 ในเดือนมีนาคม 2026 (รวม 5 กม. มากกว่า 30,000 คน) ปี 2026 เต็ม และปี 2027 จำกัดที่ 28,000 คน",
				"วันแข่งเลื่อนจาก 4 เมษายนเป็น 14 มีนาคม 2027 เปิดรับสมัคร 16 พฤศจิกายน 2026 ที่ rejestracja.maratonwarszawski.com น่าจะเป็นแบบใครสมัครก่อนได้ก่อนและราคาขึ้นเป็นระยะ เป็นส่วนหนึ่งของ SuperHalfs Europa ตั้งแต่ปี 2027")
		},
	},
}

// setText replaces a race's participants and how-to-enter text in one language.
func setText(in *EventInput, lang, participants, how string) {
	if in.Texts == nil {
		in.Texts = map[string]EventText{}
	}
	t := in.Texts[lang]
	t.Participants, t.How = participants, how
	in.Texts[lang] = t
}

// deadlineFixes correct an expected deadline of a live race once it's
// announced. Like catalogFixes they run once and are skipped when the race
// or deadline is gone.
var deadlineFixes = []struct {
	key, slug, kind string
	in              DeadlineInput
}{
	{"races_fix_2027_warsaw_half_entry_opens", "nn-polmaraton-warszawski-2027", "entry_opens", DeadlineInput{
		Kind: "entry_opens", DueDate: "2026-11-16", DatePrecision: "day",
		Texts: map[string]DeadlineText{
			"nb": {What: "Påmeldingen åpner."},
			"en": {What: "Entry opens."},
			"th": {What: "เปิดรับสมัคร"},
		},
	}},
}

func applyDeadlineFix(ctx context.Context, db *sql.DB, key, slug, kind string, in DeadlineInput) error {
	var done int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE key = ?`, key).Scan(&done); err != nil {
		return fmt.Errorf("check race fix %s: %w", key, err)
	}
	if done > 0 {
		return nil
	}
	var id int64
	err := db.QueryRowContext(ctx, `SELECT d.id FROM race_deadlines d JOIN race_events e ON e.id = d.event_id
		WHERE e.slug = ? AND d.kind = ? ORDER BY d.id LIMIT 1`, slug, kind).Scan(&id)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("race fix %s: %w", key, err)
	}
	if err == nil {
		if _, err := UpdateDeadline(ctx, db, id, in, "seed", 0); err != nil {
			return fmt.Errorf("race fix %s: %w", key, err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (key, value) VALUES (?, '1')`, key); err != nil {
		return fmt.Errorf("mark race fix %s: %w", key, err)
	}
	return nil
}

// seriesTagFixes tag the catalog races of a series. The SuperHalfs
// originals were seeded without their tag; the Europa races were already in
// the catalog when the series was announced (6 Nov 2026).
var seriesTagFixes = []struct {
	key, series string
	slugs       []string
}{
	{"races_fix_2027_tag_superhalfs", "superhalfs", []string{
		"edp-lisbon-half-2027", "generali-prague-half-marathon-2027", "copenhagen-half-marathon-2027",
		"cardiff-half-marathon-2027", "generali-berliner-halbmarathon-2027", "medio-maraton-valencia-2027",
	}},
	{"races_fix_2027_tag_superhalfs_europa", "superhalfs_europa", []string{
		"nn-polmaraton-warszawski-2027", "vienna-city-marathon-halvmaraton-2027",
		"rimi-riga-marathon-halvmaraton-2027", "adidas-stockholm-half-marathon-2027",
	}},
}

// addSeries returns a fix that adds a series tag to a race.
func addSeries(series string) func(*EventInput) {
	return func(in *EventInput) {
		for _, s := range in.Series {
			if s == series {
				return
			}
		}
		in.Series = append(in.Series, series)
	}
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
	for _, f := range deadlineFixes {
		if err := applyDeadlineFix(ctx, db, f.key, f.slug, f.kind, f.in); err != nil {
			return err
		}
	}
	for _, f := range seriesTagFixes {
		for _, slug := range f.slugs {
			if err := applyFix(ctx, db, f.key+":"+slug, slug, addSeries(f.series)); err != nil {
				return err
			}
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
		in := EventAsInput(e)
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
