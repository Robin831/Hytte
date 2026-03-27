# Hytte Salary Estimator — Income Prediction & Tax Calculation

**Status**: Planning
**Date**: 2026-03-27

---

## Overview

A salary estimation page that predicts monthly and yearly income based on hours worked, commission tiers, tax tables, and absence days. Tightly coupled with the Work Hours page — hours logged there feed directly into salary calculations here.

This replaces the "Kontroll" sheet from the Google Sheets budget.

## Core Concepts

### How Your Salary Works (from the sheet)

1. **Base salary**: Fixed monthly amount, prorated by hours worked vs standard (7.5h/day × working days in month)
2. **Commission**: 4-tier progressive structure on billed hours/revenue:
   - Tier 1: 0-60k → 0%
   - Tier 2: 60-80k → 20%
   - Tier 3: 80-100k → 40%
   - Tier 4: 100k+ → 50%
3. **Internal time**: Hours not billed to clients (meetings, admin) — tracked separately
4. **Gross = Base + Commission**
5. **Tax**: Calculated from a lookup table (Norwegian progressive tax brackets)
6. **Net = Gross - Tax**

### What the Page Should Do

- **Estimate this month's salary** based on hours worked so far (from Work Hours page) and projected remaining days
- **Predict full-year income** based on working days per month, expected utilization, and commission projections
- **Track actuals vs estimates** — log what you actually received each month
- **Show per-absence-day impact** — "each sick day costs you X kr in lost commission"
- **Vacation day tracking** — feriedager used vs remaining, feriepenger (holiday pay) accrual

## Data Model

```sql
-- Salary configuration (rarely changes)
CREATE TABLE IF NOT EXISTS salary_config (
    id              INTEGER PRIMARY KEY,
    user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    base_salary     REAL NOT NULL,           -- monthly base (full month)
    hourly_rate     REAL NOT NULL,           -- for prorating
    standard_hours  REAL NOT NULL DEFAULT 7.5, -- per day
    currency        TEXT NOT NULL DEFAULT 'NOK',
    effective_from  TEXT NOT NULL,            -- allows changing over time
    UNIQUE(user_id, effective_from)
);

-- Commission tiers (configurable)
CREATE TABLE IF NOT EXISTS salary_commission_tiers (
    id          INTEGER PRIMARY KEY,
    config_id   INTEGER NOT NULL REFERENCES salary_config(id) ON DELETE CASCADE,
    floor       REAL NOT NULL,               -- lower bound (e.g., 0, 60000)
    ceiling     REAL,                        -- upper bound (null = unlimited)
    rate        REAL NOT NULL,               -- percentage (0.0, 0.20, 0.40, 0.50)
    sort_order  INTEGER NOT NULL DEFAULT 0
);

-- Monthly salary records (actual payslip data)
CREATE TABLE IF NOT EXISTS salary_records (
    id              INTEGER PRIMARY KEY,
    user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    month           TEXT NOT NULL,            -- YYYY-MM
    working_days    INTEGER NOT NULL,         -- in this month
    hours_worked    REAL NOT NULL DEFAULT 0,  -- actual (from work hours or manual)
    billable_hours  REAL NOT NULL DEFAULT 0,  -- billed to clients
    internal_hours  REAL NOT NULL DEFAULT 0,  -- internal time
    base_amount     REAL NOT NULL DEFAULT 0,  -- prorated base salary
    commission      REAL NOT NULL DEFAULT 0,  -- calculated from tiers
    gross           REAL NOT NULL DEFAULT 0,  -- base + commission
    tax             REAL NOT NULL DEFAULT 0,
    net             REAL NOT NULL DEFAULT 0,
    vacation_days   INTEGER NOT NULL DEFAULT 0, -- feriedager taken this month
    sick_days       INTEGER NOT NULL DEFAULT 0,
    is_estimate     INTEGER NOT NULL DEFAULT 1, -- 1=projected, 0=actual (from payslip)
    notes           TEXT NOT NULL DEFAULT '',
    UNIQUE(user_id, month)
);

-- Tax brackets (Norwegian table, updated yearly)
CREATE TABLE IF NOT EXISTS salary_tax_brackets (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    year        INTEGER NOT NULL,
    income_from REAL NOT NULL,
    income_to   REAL,                        -- null = unlimited
    rate        REAL NOT NULL,               -- marginal rate
    UNIQUE(user_id, year, income_from)
);
```

## Integration with Work Hours

The salary estimator pulls data from the Work Hours system:

- **Hours worked this month**: Sum of reported hours from `work_days` for the current month
- **Working days remaining**: Business days left in the month (excluding public holidays from Work Hours)
- **Absence days**: Vacation + sick days from the leave tracker
- **Projected full-month hours**: actual + (remaining days × 7.5h)

This connection is why these two features pair naturally — Work Hours is the *input*, Salary Estimator is the *output*.

## Backend: `internal/salary/`

### Package Structure

- **models.go** — Config, CommissionTier, Record, TaxBracket structs
- **storage.go** — CRUD operations
- **calculator.go** — The estimation engine:
  - `EstimateMonth(config, hoursWorked, billableHours, workingDays) MonthEstimate`
  - `CalculateCommission(tiers, billableRevenue) float64`
  - `CalculateTax(brackets, annualGross) float64`
  - `ProjectYear(config, records) YearProjection`
  - `AbsenceDayCost(config, month) float64` — what one absence day costs
- **handlers.go** — HTTP handlers
- **norwegian_tax.go** — Default Norwegian tax table for current year

### API Endpoints

```
GET    /api/salary/config                    — current salary config
PUT    /api/salary/config                    — update config (base, rate, tiers)

GET    /api/salary/estimate/current          — this month's estimate (uses work hours data)
GET    /api/salary/estimate/month?month=YYYY-MM — estimate for specific month
GET    /api/salary/estimate/year?year=2026   — full year projection

GET    /api/salary/records                   — all monthly records
PUT    /api/salary/records/{month}           — update with actual payslip data
POST   /api/salary/records/{month}/confirm   — mark estimate as actual

GET    /api/salary/tax-table?year=2026       — current tax brackets
PUT    /api/salary/tax-table                 — update tax brackets

GET    /api/salary/absence-cost              — per-day absence impact
GET    /api/salary/vacation                  — vacation days used/remaining/accrued
```

## Frontend: SalaryPage.tsx

### This Month Card (hero)

```
┌─────────────────────────────────────────┐
│ March 2026                    Est. ⏳    │
│                                         │
│ Gross    X kr    (base X + commission X) │
│ Tax     -X kr    (34.2%)                │
│ ─────────────────                       │
│ Net      X kr                           │
│                                         │
│ Hours: 142.5 / 157.5  (90.5%)          │
│ Billable: 127.5h                        │
│ Working days: 15 done, 6 remaining      │
│ Absence impact: -X kr/day               │
└─────────────────────────────────────────┘
```

When actual payslip data is entered, the card switches from "Est. ⏳" to "Actual ✓" and shows the variance.

### Commission Breakdown

Visual breakdown of which tier is active:
```
Tier 1: 0-60k     ████████████████████  0%    →  0 kr
Tier 2: 60-80k    ████████████░░░░░░░░  20%   →  X kr
Tier 3: 80-100k   ░░░░░░░░░░░░░░░░░░░░  40%   →  (projected)
Tier 4: 100k+     ░░░░░░░░░░░░░░░░░░░░  50%   →  -
```

Progress bar showing how far into each tier you are this month.

### Year Overview

Table: Jan-Dec showing per month:
- Working days, hours worked, billable hours
- Base, commission, gross, tax, net
- Estimate vs actual flag
- Row highlighting: past months (actual or confirmed estimate), current (live), future (projected)

Year totals at bottom. Comparison with previous year if data exists.

### Utilization Chart

Line chart (Recharts) showing monthly utilization % (billable hours / standard hours). Target line at 100%. This is the metric that drives commission.

### Vacation Tracker (compact)

- Feriedager: X of 25 used
- Feriepenger accrued: X kr (10.2% of gross)
- Remaining days to plan

## Connection to Budget

The salary estimator feeds into the budget system:
- Monthly net income → budget's income category (auto-populated)
- Year projection → annual budget planning
- Actual payslip → reconcile budget vs actual income

This connection can be a simple API call from the budget page, or a shared "income" concept.

## Connection to Ny Bolig (Housing)

The sheet's "Ny bolig" tab calculates loan servicing capacity from income. This could be:
- A section on the salary page showing mortgage affordability
- Or integrated into the budget page's loan/amortization view

**Recommendation**: Keep it in the budget system as a "loans" section with amortization calculations. The salary page provides the income; the budget page shows where it goes (including loan payments).

## Phase Plan

### Phase 1: Core estimation
- Salary config (base, hourly rate, tiers)
- Monthly estimation from hours worked
- Commission tier calculation
- Basic tax estimation (flat % or simple lookup)
- This-month card on SalaryPage.tsx
- Integration with Work Hours data

### Phase 2: Year projection + actuals
- Full year projection (12 months)
- Enter actual payslip data to replace estimates
- Year overview table
- Utilization chart
- Estimate vs actual variance display

### Phase 3: Tax + vacation
- Norwegian tax bracket table (proper progressive calculation)
- Vacation day tracking + feriepenger accrual
- Per-absence-day cost calculation
- Feed net income into budget system

## Privacy & Security

- Feature-gated (`salary`)
- Encrypted at rest (salary data is highly sensitive)
- User-scoped — strictly personal
- Adults only
- No employer integration — personal tracking tool

## Decisions

1. **Tightly coupled with Work Hours**: Hours flow automatically from work_days table.
2. **Commission tiers configurable**: Not hardcoded — set up via config so it works if the tier structure changes.
3. **Tax table updatable yearly**: Norwegian tax brackets change each year — make it easy to update.
4. **Estimate→Actual flow**: Each month starts as an estimate, can be confirmed/updated with actual payslip data.
