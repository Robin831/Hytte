package salary

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE salary_config (
			id                   INTEGER PRIMARY KEY,
			user_id              INTEGER NOT NULL,
			base_salary          REAL NOT NULL DEFAULT 0,
			hourly_rate          REAL NOT NULL DEFAULT 0,
			internal_hourly_rate REAL NOT NULL DEFAULT 0,
			standard_hours       REAL NOT NULL DEFAULT 7.5,
			currency             TEXT NOT NULL DEFAULT 'NOK',
			effective_from       TEXT NOT NULL,
			UNIQUE(user_id, effective_from)
		);
		CREATE TABLE salary_commission_tiers (
			id        INTEGER PRIMARY KEY,
			config_id INTEGER NOT NULL,
			floor     REAL NOT NULL DEFAULT 0,
			ceiling   REAL NOT NULL DEFAULT 0,
			rate      REAL NOT NULL DEFAULT 0,
			UNIQUE(config_id, floor)
		);
		CREATE TABLE salary_tax_brackets (
			id          INTEGER PRIMARY KEY,
			user_id     INTEGER NOT NULL,
			year        INTEGER NOT NULL,
			income_from REAL NOT NULL DEFAULT 0,
			income_to   REAL NOT NULL DEFAULT 0,
			rate        REAL NOT NULL DEFAULT 0
		);
		CREATE TABLE salary_records (
			id             INTEGER PRIMARY KEY,
			user_id        INTEGER NOT NULL,
			month          TEXT NOT NULL,
			working_days   INTEGER NOT NULL DEFAULT 0,
			hours_worked   REAL NOT NULL DEFAULT 0,
			billable_hours REAL NOT NULL DEFAULT 0,
			internal_hours REAL NOT NULL DEFAULT 0,
			base_amount    REAL NOT NULL DEFAULT 0,
			commission     REAL NOT NULL DEFAULT 0,
			gross          REAL NOT NULL DEFAULT 0,
			tax            REAL NOT NULL DEFAULT 0,
			net            REAL NOT NULL DEFAULT 0,
			vacation_days         INTEGER NOT NULL DEFAULT 0,
			sick_days             INTEGER NOT NULL DEFAULT 0,
			is_estimate           INTEGER NOT NULL DEFAULT 1,
			budget_transaction_id INTEGER,
			UNIQUE(user_id, month)
		);
		CREATE TABLE work_days (
			id      INTEGER PRIMARY KEY,
			user_id INTEGER NOT NULL,
			date    TEXT NOT NULL
		);
		CREATE TABLE work_sessions (
			id          INTEGER PRIMARY KEY,
			day_id      INTEGER NOT NULL,
			start_time  TEXT NOT NULL,
			end_time    TEXT NOT NULL,
			is_internal INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE work_leave_days (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL,
			date       TEXT NOT NULL,
			leave_type TEXT NOT NULL,
			note       TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			UNIQUE(user_id, date)
		);
		CREATE TABLE salary_trekktabell_params (
			id                    INTEGER PRIMARY KEY,
			user_id               INTEGER NOT NULL,
			year                  INTEGER NOT NULL,
			minstefradrag_rate    REAL NOT NULL DEFAULT 0.46,
			minstefradrag_min     REAL NOT NULL DEFAULT 31800,
			minstefradrag_max     REAL NOT NULL DEFAULT 104450,
			personfradrag         REAL NOT NULL DEFAULT 108550,
			alminnelig_skatt_rate REAL NOT NULL DEFAULT 0.22,
			trygdeavgift          REAL NOT NULL DEFAULT 0.079,
			trinnskatt_json       TEXT NOT NULL DEFAULT '[]',
			UNIQUE(user_id, year)
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func TestSaveAndGetConfig(t *testing.T) {
	db := setupTestDB(t)

	cfg := &Config{
		UserID:        1,
		BaseSalary:    60000,
		HourlyRate:    500,
		StandardHours: 7.5,
		Currency:      "NOK",
		EffectiveFrom: "2026-01-01",
	}

	if err := SaveConfig(db, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if cfg.ID == 0 {
		t.Error("SaveConfig should set ID on insert")
	}

	got, err := GetConfig(db, 1)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if got == nil {
		t.Fatal("GetConfig returned nil, want config")
	}
	if got.BaseSalary != 60000 {
		t.Errorf("BaseSalary = %v, want 60000", got.BaseSalary)
	}
	if got.Currency != "NOK" {
		t.Errorf("Currency = %q, want NOK", got.Currency)
	}
}

func TestGetConfig_NoRows(t *testing.T) {
	db := setupTestDB(t)

	got, err := GetConfig(db, 999)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if got != nil {
		t.Errorf("GetConfig returned %+v, want nil", got)
	}
}

func TestSaveConfig_Upsert(t *testing.T) {
	db := setupTestDB(t)

	cfg := &Config{
		UserID:        1,
		BaseSalary:    50000,
		StandardHours: 7.5,
		Currency:      "NOK",
		EffectiveFrom: "2026-01-01",
	}
	if err := SaveConfig(db, cfg); err != nil {
		t.Fatalf("SaveConfig insert: %v", err)
	}

	// Upsert with same (user_id, effective_from) should update.
	cfg.BaseSalary = 65000
	if err := SaveConfig(db, cfg); err != nil {
		t.Fatalf("SaveConfig upsert: %v", err)
	}

	got, err := GetConfig(db, 1)
	if err != nil {
		t.Fatalf("GetConfig after upsert: %v", err)
	}
	if got.BaseSalary != 65000 {
		t.Errorf("BaseSalary after upsert = %v, want 65000", got.BaseSalary)
	}
}

func TestSeedDefaultTiers(t *testing.T) {
	db := setupTestDB(t)

	cfg := &Config{UserID: 1, StandardHours: 7.5, Currency: "NOK", EffectiveFrom: "2026-01-01"}
	if err := SaveConfig(db, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := SeedDefaultTiers(db, cfg.ID); err != nil {
		t.Fatalf("SeedDefaultTiers: %v", err)
	}

	tiers, err := GetCommissionTiers(db, cfg.ID)
	if err != nil {
		t.Fatalf("GetCommissionTiers: %v", err)
	}
	if len(tiers) != 4 {
		t.Fatalf("len(tiers) = %d, want 4", len(tiers))
	}
	if tiers[0].Floor != 0 || tiers[0].Ceiling != 60000 || tiers[0].Rate != 0 {
		t.Errorf("tier[0] = %+v, want floor=0 ceiling=60000 rate=0", tiers[0])
	}
	if tiers[3].Floor != 100000 || tiers[3].Ceiling != 0 || tiers[3].Rate != 0.50 {
		t.Errorf("tier[3] = %+v, want floor=100000 ceiling=0 rate=0.50", tiers[3])
	}
}

func TestSeedDefaultTiers_Idempotent(t *testing.T) {
	db := setupTestDB(t)

	cfg := &Config{UserID: 1, StandardHours: 7.5, Currency: "NOK", EffectiveFrom: "2026-01-01"}
	if err := SaveConfig(db, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := SeedDefaultTiers(db, cfg.ID); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if err := SeedDefaultTiers(db, cfg.ID); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	tiers, err := GetCommissionTiers(db, cfg.ID)
	if err != nil {
		t.Fatalf("GetCommissionTiers: %v", err)
	}
	if len(tiers) != 4 {
		t.Errorf("len(tiers) = %d after double seed, want 4", len(tiers))
	}
}

func TestSaveAndGetRecords(t *testing.T) {
	db := setupTestDB(t)

	r := &Record{
		UserID:        1,
		Month:         "2026-03",
		WorkingDays:   22,
		HoursWorked:   165,
		BillableHours: 150,
		BaseAmount:    60000,
		Gross:         63000,
		Tax:           18900,
		Net:           44100,
		IsEstimate:    true,
	}

	if err := SaveRecord(db, r); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	if r.ID == 0 {
		t.Error("SaveRecord should set ID on insert")
	}

	records, err := GetRecords(db, 1, 2026)
	if err != nil {
		t.Fatalf("GetRecords: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	got := records[0]
	if got.Month != "2026-03" {
		t.Errorf("Month = %q, want 2026-03", got.Month)
	}
	if got.WorkingDays != 22 {
		t.Errorf("WorkingDays = %d, want 22", got.WorkingDays)
	}
	if !got.IsEstimate {
		t.Error("IsEstimate should be true")
	}
}

func TestSaveRecord_Upsert(t *testing.T) {
	db := setupTestDB(t)

	r := &Record{UserID: 1, Month: "2026-03", WorkingDays: 22, HoursWorked: 165, IsEstimate: true}
	if err := SaveRecord(db, r); err != nil {
		t.Fatalf("SaveRecord insert: %v", err)
	}

	// Upsert same month — should update.
	r.HoursWorked = 120
	r.IsEstimate = false
	if err := SaveRecord(db, r); err != nil {
		t.Fatalf("SaveRecord upsert: %v", err)
	}

	records, err := GetRecords(db, 1, 2026)
	if err != nil {
		t.Fatalf("GetRecords: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d after upsert, want 1", len(records))
	}
	if records[0].HoursWorked != 120 {
		t.Errorf("HoursWorked = %v, want 120", records[0].HoursWorked)
	}
	if records[0].IsEstimate {
		t.Error("IsEstimate should be false after upsert")
	}
}

func TestGetRecord_NotFound(t *testing.T) {
	db := setupTestDB(t)

	rec, err := GetRecord(db, 1, "2026-01")
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if rec != nil {
		t.Errorf("GetRecord returned %+v, want nil", rec)
	}
}

func TestGetRecord_Found(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`
		INSERT INTO salary_records
			(user_id, month, working_days, hours_worked, billable_hours, internal_hours,
			 base_amount, commission, gross, tax, net, vacation_days, sick_days, is_estimate)
		VALUES (1, '2026-01', 22, 165.0, 140.0, 25.0, 60000, 2000, 62000, 12000, 50000, 2, 1, 0)
	`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	rec, err := GetRecord(db, 1, "2026-01")
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if rec == nil {
		t.Fatal("GetRecord returned nil, want record")
	}
	if rec.Month != "2026-01" {
		t.Errorf("Month = %q, want 2026-01", rec.Month)
	}
	if rec.BillableHours != 140.0 {
		t.Errorf("BillableHours = %v, want 140.0", rec.BillableHours)
	}
	if rec.InternalHours != 25.0 {
		t.Errorf("InternalHours = %v, want 25.0", rec.InternalHours)
	}
	if rec.IsEstimate {
		t.Error("IsEstimate should be false")
	}
}

func TestGetRecords_Empty(t *testing.T) {
	db := setupTestDB(t)

	records, err := GetRecords(db, 1, 2026)
	if err != nil {
		t.Fatalf("GetRecords: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("len(records) = %d, want 0", len(records))
	}
}

func TestGetTaxBrackets(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`
		INSERT INTO salary_tax_brackets (user_id, year, income_from, income_to, rate)
		VALUES (1, 2026, 0, 200000, 0.22),
		       (1, 2026, 200000, 0, 0.32)
	`)
	if err != nil {
		t.Fatalf("insert brackets: %v", err)
	}

	brackets, err := GetTaxBrackets(db, 1, 2026)
	if err != nil {
		t.Fatalf("GetTaxBrackets: %v", err)
	}
	if len(brackets) != 2 {
		t.Fatalf("len(brackets) = %d, want 2", len(brackets))
	}
	if brackets[0].Rate != 0.22 {
		t.Errorf("brackets[0].Rate = %v, want 0.22", brackets[0].Rate)
	}
	if brackets[1].IncomeTo != 0 {
		t.Errorf("brackets[1].IncomeTo = %v, want 0 (unbounded)", brackets[1].IncomeTo)
	}
}

func TestGetTaxBrackets_WrongYear(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO salary_tax_brackets (user_id, year, income_from, income_to, rate) VALUES (1, 2025, 0, 0, 0.22)`)
	if err != nil {
		t.Fatalf("insert bracket: %v", err)
	}

	brackets, err := GetTaxBrackets(db, 1, 2026)
	if err != nil {
		t.Fatalf("GetTaxBrackets: %v", err)
	}
	if len(brackets) != 0 {
		t.Errorf("len(brackets) = %d, want 0 (different year)", len(brackets))
	}
}

func TestGetRecordForMonth(t *testing.T) {
	t.Run("returns nil when no record exists", func(t *testing.T) {
		db := setupTestDB(t)
		got, err := GetRecordForMonth(db, 1, "2026-03")
		if err != nil {
			t.Fatalf("GetRecordForMonth: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("returns saved record", func(t *testing.T) {
		db := setupTestDB(t)
		r := &Record{
			UserID:        1,
			Month:         "2026-03",
			WorkingDays:   22,
			HoursWorked:   165,
			BillableHours: 140,
			BaseAmount:    60000,
			Commission:    3000,
			Gross:         63000,
			Tax:           18000,
			Net:           45000,
			VacationDays:  2,
			SickDays:      1,
			IsEstimate:    true,
		}
		if err := SaveRecord(db, r); err != nil {
			t.Fatalf("SaveRecord: %v", err)
		}

		got, err := GetRecordForMonth(db, 1, "2026-03")
		if err != nil {
			t.Fatalf("GetRecordForMonth: %v", err)
		}
		if got == nil {
			t.Fatal("expected record, got nil")
		}
		if got.Month != "2026-03" {
			t.Errorf("Month = %q, want 2026-03", got.Month)
		}
		if got.WorkingDays != 22 {
			t.Errorf("WorkingDays = %d, want 22", got.WorkingDays)
		}
		if got.Commission != 3000 {
			t.Errorf("Commission = %v, want 3000", got.Commission)
		}
		if got.VacationDays != 2 {
			t.Errorf("VacationDays = %d, want 2", got.VacationDays)
		}
		if got.SickDays != 1 {
			t.Errorf("SickDays = %d, want 1", got.SickDays)
		}
		if !got.IsEstimate {
			t.Error("IsEstimate should be true")
		}
	})

	t.Run("does not return record for different user", func(t *testing.T) {
		db := setupTestDB(t)
		r := &Record{UserID: 1, Month: "2026-03", WorkingDays: 22, IsEstimate: true}
		if err := SaveRecord(db, r); err != nil {
			t.Fatalf("SaveRecord: %v", err)
		}

		got, err := GetRecordForMonth(db, 2, "2026-03")
		if err != nil {
			t.Fatalf("GetRecordForMonth: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil for different user, got %+v", got)
		}
	})

	t.Run("does not return record for different month", func(t *testing.T) {
		db := setupTestDB(t)
		r := &Record{UserID: 1, Month: "2026-03", WorkingDays: 22, IsEstimate: true}
		if err := SaveRecord(db, r); err != nil {
			t.Fatalf("SaveRecord: %v", err)
		}

		got, err := GetRecordForMonth(db, 1, "2026-04")
		if err != nil {
			t.Fatalf("GetRecordForMonth: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil for different month, got %+v", got)
		}
	})
}

func TestGetHoursWorked(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO work_days (id, user_id, date) VALUES (1, 1, '2026-03-01')`)
	if err != nil {
		t.Fatalf("insert work_day: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO work_sessions (day_id, start_time, end_time)
		VALUES (1, '08:00', '16:00'),
		       (1, '17:00', '18:30')
	`)
	if err != nil {
		t.Fatalf("insert work_sessions: %v", err)
	}

	// 08:00-16:00 = 480 min, 17:00-18:30 = 90 min → 570 min = 9.5 h
	hours, err := GetHoursWorked(db, 1, "2026-03")
	if err != nil {
		t.Fatalf("GetHoursWorked: %v", err)
	}
	if hours != 9.5 {
		t.Errorf("GetHoursWorked = %v, want 9.5", hours)
	}
}

func TestGetHoursWorked_NoSessions(t *testing.T) {
	db := setupTestDB(t)

	hours, err := GetHoursWorked(db, 1, "2026-03")
	if err != nil {
		t.Fatalf("GetHoursWorked: %v", err)
	}
	if hours != 0 {
		t.Errorf("GetHoursWorked = %v, want 0", hours)
	}
}

func TestGetInternalHoursWorked(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO work_days (id, user_id, date) VALUES (1, 1, '2026-03-01')`)
	if err != nil {
		t.Fatalf("insert work_day: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO work_sessions (day_id, start_time, end_time, is_internal)
		VALUES (1, '09:00', '10:00', 1),
		       (1, '14:00', '15:30', 1),
		       (1, '08:00', '17:00', 0)
	`)
	if err != nil {
		t.Fatalf("insert work_sessions: %v", err)
	}

	// 09:00-10:00 = 60 min, 14:00-15:30 = 90 min → 150 min = 2.5 h (external session excluded)
	hours, err := GetInternalHoursWorked(db, 1, "2026-03")
	if err != nil {
		t.Fatalf("GetInternalHoursWorked: %v", err)
	}
	if hours != 2.5 {
		t.Errorf("GetInternalHoursWorked = %v, want 2.5", hours)
	}
}

func TestGetInternalHoursWorked_NoSessions(t *testing.T) {
	db := setupTestDB(t)

	hours, err := GetInternalHoursWorked(db, 1, "2026-03")
	if err != nil {
		t.Fatalf("GetInternalHoursWorked: %v", err)
	}
	if hours != 0 {
		t.Errorf("GetInternalHoursWorked = %v, want 0", hours)
	}
}

func TestGetInternalHoursWorked_OnlyExternal(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO work_days (id, user_id, date) VALUES (1, 1, '2026-03-01')`)
	if err != nil {
		t.Fatalf("insert work_day: %v", err)
	}
	_, err = db.Exec(`INSERT INTO work_sessions (day_id, start_time, end_time, is_internal) VALUES (1, '08:00', '16:00', 0)`)
	if err != nil {
		t.Fatalf("insert work_session: %v", err)
	}

	hours, err := GetInternalHoursWorked(db, 1, "2026-03")
	if err != nil {
		t.Fatalf("GetInternalHoursWorked: %v", err)
	}
	if hours != 0 {
		t.Errorf("GetInternalHoursWorked = %v, want 0 (no internal sessions)", hours)
	}
}
