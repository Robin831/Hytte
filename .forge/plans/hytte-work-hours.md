# Hytte Work Hours — Time Tracking & Flex Calculation

**Status**: Planning
**Date**: 2026-03-27

---

## Overview

A simple but precise work hours calculator. Log start/end times for each work session in a day, tick deductions (lunch, kindergarten drop-off, errands), and get your hours rounded to the nearest half-hour with leftover minutes tracked in a flex pool.

## Core Concepts

### Daily Entry

A work day consists of:
- **Sessions**: one or more start/end time pairs (e.g., 06:00-08:00, 08:45-15:00)
- **Lunch**: checkbox (fixed 30 min deduction, since Norwegian standard)
- **Custom deductions**: named time spans to subtract (kindergarten, doctor, etc.)
- **Gross minutes**: total minutes across all sessions
- **Deductions**: lunch + custom deductions in minutes
- **Net minutes**: gross - deductions
- **Reported hours**: net rounded down to nearest 0.5h (e.g., 457 min → 7.5h)
- **Remainder**: leftover minutes that go to the flex pool (457 - 450 = +7 min)

### Rounding & Flex Pool

The key insight: you work in real minutes but report in half-hours. The gap accumulates.

Example over a week:
| Day | Net min | Reported | Remainder |
|-----|---------|----------|-----------|
| Mon | 457     | 7.5h     | +7 min    |
| Tue | 438     | 7.0h     | +8 min    |
| Wed | 462     | 7.5h     | +12 min   |
| Thu | 445     | 7.0h     | +15 min   |
| Fri | 420     | 7.0h     | +0 min    |
| **Week** | **2222** | **36.0h** | **+42 min** |

When the pool hits +30 min, you've earned an extra half-hour. When it hits -30 min, you owe one. The pool carries across weeks within a month (or rolling — your preference).

**Recommendation**: Show the pool prominently — "Flex: +42 min (12 min to next half-hour)". This is the number you actually care about. Consider configurable rounding: some workplaces round to nearest quarter-hour instead of half-hour.

### Standard Day

Norwegian full-time is 7.5h/day (37.5h/week). Consider showing how each day compares to the standard:
- 7.5h → "On target"
- 8.0h → "+30 min overtime"
- 7.0h → "-30 min short"

This feeds into a weekly/monthly balance: "This week: 37.5h target, 36.0h worked, -1.5h balance."

## Data Model

```sql
-- One row per work day
CREATE TABLE IF NOT EXISTS work_days (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    date        TEXT NOT NULL,            -- YYYY-MM-DD
    lunch       INTEGER NOT NULL DEFAULT 0,  -- 1 = 30 min deducted
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT '',
    UNIQUE(user_id, date)
);

-- Work sessions (multiple per day)
CREATE TABLE IF NOT EXISTS work_sessions (
    id          INTEGER PRIMARY KEY,
    day_id      INTEGER NOT NULL REFERENCES work_days(id) ON DELETE CASCADE,
    start_time  TEXT NOT NULL,            -- HH:MM (24h)
    end_time    TEXT NOT NULL,            -- HH:MM (24h)
    sort_order  INTEGER NOT NULL DEFAULT 0
);

-- Custom deductions per day
CREATE TABLE IF NOT EXISTS work_deductions (
    id          INTEGER PRIMARY KEY,
    day_id      INTEGER NOT NULL REFERENCES work_days(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,            -- "Kindergarten", "Doctor", etc.
    minutes     INTEGER NOT NULL,         -- duration in minutes
    preset_id   INTEGER REFERENCES work_deduction_presets(id)
);

-- Reusable deduction presets (so you don't type "Kindergarten" every day)
CREATE TABLE IF NOT EXISTS work_deduction_presets (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,            -- "Kindergarten drop-off"
    default_minutes INTEGER NOT NULL DEFAULT 15,
    icon        TEXT NOT NULL DEFAULT '🚗',
    sort_order  INTEGER NOT NULL DEFAULT 0,
    active      INTEGER NOT NULL DEFAULT 1
);

-- User settings for work hours
-- Stored in user_preferences:
--   work_hours_standard_day: 450 (minutes, default 7.5h)
--   work_hours_standard_week: 2250 (minutes, default 37.5h)
--   work_hours_rounding: 30 (minutes, default half-hour)
--   work_hours_lunch_minutes: 30
--   work_hours_flex_reset: "monthly" or "never"
```

**Recommendation**: Keep the schema simple. The calculation logic (rounding, flex pool) should be computed, not stored. Store raw times, compute everything else.

## Backend: `internal/workhours/`

### Package Structure

- **models.go** — WorkDay, Session, Deduction, Preset, DaySummary, WeekSummary structs
- **storage.go** — CRUD for days, sessions, deductions, presets
- **calculator.go** — The math:
  - `CalculateDay(day) DaySummary` — gross, deductions, net, reported, remainder
  - `CalculateWeek(days) WeekSummary` — totals, flex pool, balance vs standard
  - `CalculateMonth(days) MonthSummary` — same but monthly
  - `FlexPool(days) int` — cumulative remainder minutes
- **handlers.go** — HTTP handlers

### API Endpoints

```
GET    /api/workhours/day?date=2026-03-27     — get day entry with sessions + deductions
PUT    /api/workhours/day                      — create/update day (upsert)
DELETE /api/workhours/day?date=2026-03-27     — delete day entry

POST   /api/workhours/day/session              — add session to day
PUT    /api/workhours/day/session/{id}         — update session
DELETE /api/workhours/day/session/{id}         — remove session

POST   /api/workhours/day/deduction            — add deduction to day
DELETE /api/workhours/day/deduction/{id}       — remove deduction

GET    /api/workhours/presets                  — list deduction presets
POST   /api/workhours/presets                  — create preset
PUT    /api/workhours/presets/{id}             — update preset
DELETE /api/workhours/presets/{id}             — deactivate preset

GET    /api/workhours/summary/week?date=2026-03-27  — week summary containing this date
GET    /api/workhours/summary/month?month=2026-03   — month summary
GET    /api/workhours/flex?month=2026-03            — flex pool balance
```

## Frontend: WorkHoursPage.tsx

### Daily View (Main)

**Date selector** at top: ← 2026-03-27 (Thu) →  (quick nav: today, yesterday, arrow keys)

**Sessions section:**
```
┌─────────────────────────────────────────┐
│ Sessions                          [+ Add]│
│                                          │
│  06:00  ──────────  08:00    2h 0min  ✕ │
│  08:45  ──────────  15:00    6h 15min ✕ │
│                                          │
│  Gross: 8h 15min                         │
└──────────────────────────────────────────┘
```

Time inputs: simple HH:MM fields. Consider a quick-input pattern where typing "0600" auto-formats to "06:00". Or a dropdown with 15-minute increments.

**Deductions section:**
```
┌─────────────────────────────────────────┐
│ Deductions                               │
│                                          │
│  ☑ Lunch                      -30 min    │
│  🚗 Kindergarten              -15 min  ✕ │
│                                [+ Add]   │
│                                          │
│  Total deductions: -45 min               │
└──────────────────────────────────────────┘
```

Lunch is always shown as a checkbox. Custom deductions show preset buttons for quick add (tap "Kindergarten" → adds 15 min deduction), plus a manual "add custom" option where you type name + minutes.

**Summary card:**
```
┌─────────────────────────────────────────┐
│  Gross      8h 15min                     │
│  Deductions -45 min                      │
│  Net        7h 30min                     │
│  ─────────────────────                   │
│  Reported   7.5h  ✓ (on target)          │
│  Remainder  +0 min → flex pool           │
│                                          │
│  Flex pool: +42 min                      │
│  (12 min to next half-hour)              │
└──────────────────────────────────────────┘
```

### Week Overview

Table showing Mon-Fri (or Mon-Sun if you work weekends):

| Day | Start | End | Net | Reported | +/- |
|-----|-------|-----|-----|----------|-----|
| Mon | 06:00 | 15:00 | 7h 27min | 7.5h | - |
| Tue | 07:00 | 15:30 | 7h 0min | 7.0h | -30min |
| Wed | 06:30 | 15:15 | 7h 42min | 7.5h | +12min |
| ... | | | | | |
| **Total** | | | **36h 15min** | **36.0h** | **-1.5h** |

Flex pool running total at the bottom.

### Month Overview

- Calendar grid showing hours per day (color coded: green = 7.5h, yellow = short, blue = over)
- Monthly totals: worked, target, balance
- Flex pool trend chart (line chart of cumulative flex over the month)

### Settings

- Standard work day (default 7.5h)
- Rounding increment (default 30 min, option for 15 min)
- Lunch duration (default 30 min)
- Deduction presets management
- Flex pool reset policy (monthly, quarterly, never)

## UX Recommendations

**Quick entry is everything.** You'll use this daily — it must be fast. Recommendations:

1. **"Start" button**: One tap to log "I'm starting now" (captures current time as session start). Tap again to end. Like a punch clock.

2. **Yesterday prefill**: If yesterday had similar sessions, offer "Copy yesterday's times?" as a starting point.

3. **Keyboard shortcuts**: On desktop, typing should be enough — Tab through fields, Enter to save.

4. **Mobile first**: This is a "waiting for the bus" kind of app. Big touch targets, minimal scrolling.

5. **Preset deductions as toggles**: Show your saved presets as toggle chips. Tap "Kindergarten" to add/remove the deduction. Much faster than a form.

## i18n

New namespace: `workhours.json`
- `workhours.title` = "Work Hours" / "Arbeidstimer" / "ชั่วโมงทำงาน"
- `workhours.lunch` = "Lunch" / "Lunsj" / "อาหารกลางวัน"
- `workhours.flexPool` = "Flex pool" / "Fleksipool"
- `workhours.reported` = "Reported" / "Rapportert"
- `workhours.onTarget` = "On target" / "I mål"
- `workhours.overtime` = "Overtime" / "Overtid"

## Phase Plan

### Phase 1: Daily entry + calculation
- Data model + storage
- Daily entry: sessions, lunch checkbox, manual deductions
- Calculation: gross, net, reported (half-hour rounding), remainder
- Summary card with flex pool
- Feature flag + sidebar + i18n

### Phase 2: Week & month views
- Week overview table
- Month calendar with daily hours
- Weekly/monthly totals and balance vs standard
- Flex pool trend

### Phase 3: Presets & polish
- Deduction presets (create, manage, quick-add)
- "Copy yesterday" feature
- Start/stop punch clock button
- Settings page (standard day, rounding, lunch duration)
- Export (CSV for submitting to employer if needed)

## Privacy & Security

- Feature-gated (`work_hours`)
- User-scoped — strictly personal data
- Encrypted at rest (work times are sensitive)
- No employer integration — this is your personal tracker
- Adults only (no reason for kids to see this)

## Decisions

1. **Weekends**: No — week view shows Mon-Fri only.
2. **Overtime distinction**: No — just total hours, no regular/overtime split.
3. **Flex pool reset**: Manual — user decides when to reset the accumulated minutes (button in settings or on the flex display).
4. **Public holidays**: Yes — show Norwegian public holidays automatically (mark them in the calendar, auto-fill as "day off"). Use a static list or a Norwegian holiday library.
5. **Days off tracker**: Yes — track vacation days remaining, sick days, and other leave types. Show balance ("18 of 25 vacation days used").
