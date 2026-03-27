# Hytte Budget — Personal Finance Tracker

**Status**: Planning
**Date**: 2026-03-26

---

## Overview

Build a budgeting system in Hytte, inspired by your existing Google Sheets setup. This is the most complex of the new features — it touches recurring transactions, categorization, forecasting, and multi-currency — so we'll plan carefully and build incrementally.

## Core Concepts

### What a Budget System Needs

1. **Accounts** — where money lives (checking, savings, credit card, cash)
2. **Categories** — what money is for (groceries, rent, transport, entertainment)
3. **Budgets** — how much you plan to spend per category per period
4. **Transactions** — actual money movement (income, expenses, transfers)
5. **Recurring items** — salary, rent, subscriptions (auto-generated each period)
6. **Reporting** — where did the money go? Am I on track?

### What Makes Your Use Case Specific

Since you already have a sophisticated Google Sheets budget, you've likely solved problems that generic budget apps ignore. Before we build, I'd recommend we extract the *structure* from your spreadsheet:

- What are your categories? (Probably more granular than "Food" — likely "Groceries", "Restaurants", "Takeaway", etc.)
- Do you budget monthly, weekly, or both?
- Do you track by person (your spending vs partner vs shared)?
- Do you handle NOK + THB (or other currencies)?
- Is the sheet formula-heavy (calculated fields, rolling averages)?

**Recommendation**: Let me look at your Google Sheet structure before we finalize this plan. You can share it or describe the layout — I'll design the data model to match your mental model rather than forcing you into a generic one.

## Data Model (Draft)

### Tables

```sql
-- Where money lives
CREATE TABLE IF NOT EXISTS budget_accounts (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,              -- "DNB Checking", "Wise THB"
    type        TEXT NOT NULL DEFAULT 'checking',  -- checking, savings, credit, cash, investment
    currency    TEXT NOT NULL DEFAULT 'NOK',
    balance     REAL NOT NULL DEFAULT 0,    -- current balance (denormalized)
    icon        TEXT NOT NULL DEFAULT '🏦',
    sort_order  INTEGER NOT NULL DEFAULT 0,
    archived    INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT ''
);

-- What money is for
CREATE TABLE IF NOT EXISTS budget_categories (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,              -- "Groceries"
    group_name  TEXT NOT NULL DEFAULT '',   -- "Food & Drink" (parent grouping)
    icon        TEXT NOT NULL DEFAULT '📦',
    color       TEXT NOT NULL DEFAULT '',   -- hex color for charts
    is_income   INTEGER NOT NULL DEFAULT 0,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    archived    INTEGER NOT NULL DEFAULT 0
);

-- How much to spend per category per month
CREATE TABLE IF NOT EXISTS budget_limits (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category_id INTEGER NOT NULL REFERENCES budget_categories(id) ON DELETE CASCADE,
    amount      REAL NOT NULL,
    currency    TEXT NOT NULL DEFAULT 'NOK',
    period      TEXT NOT NULL DEFAULT 'monthly',  -- monthly, weekly, yearly
    effective_from TEXT NOT NULL,  -- allows changing budgets over time
    UNIQUE(user_id, category_id, effective_from)
);

-- Actual money movement
CREATE TABLE IF NOT EXISTS budget_transactions (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id  INTEGER NOT NULL REFERENCES budget_accounts(id),
    category_id INTEGER REFERENCES budget_categories(id),
    amount      REAL NOT NULL,              -- negative = expense, positive = income
    currency    TEXT NOT NULL DEFAULT 'NOK',
    description TEXT NOT NULL DEFAULT '',
    date        TEXT NOT NULL,              -- YYYY-MM-DD
    recurring_id INTEGER REFERENCES budget_recurring(id),  -- if auto-generated
    is_transfer INTEGER NOT NULL DEFAULT 0,
    transfer_to INTEGER REFERENCES budget_accounts(id),    -- destination account
    tags        TEXT NOT NULL DEFAULT '',   -- comma-separated free-form tags
    created_at  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_budget_tx_user_date
    ON budget_transactions(user_id, date);
CREATE INDEX IF NOT EXISTS idx_budget_tx_category
    ON budget_transactions(user_id, category_id, date);

-- Auto-generated transactions
CREATE TABLE IF NOT EXISTS budget_recurring (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id  INTEGER NOT NULL REFERENCES budget_accounts(id),
    category_id INTEGER REFERENCES budget_categories(id),
    amount      REAL NOT NULL,
    currency    TEXT NOT NULL DEFAULT 'NOK',
    description TEXT NOT NULL,
    frequency   TEXT NOT NULL,    -- monthly, weekly, biweekly, yearly, quarterly
    day_of_month INTEGER,        -- 1-28 for monthly (avoid 29-31 issues)
    day_of_week  INTEGER,        -- 0=Mon for weekly/biweekly
    start_date  TEXT NOT NULL,
    end_date    TEXT,            -- null = no end
    last_generated TEXT,         -- last date a transaction was auto-created
    active      INTEGER NOT NULL DEFAULT 1
);
```

### Currency Handling

**Recommendation**: Store all amounts in their original currency. Don't convert everything to NOK — you'll lose accuracy and confuse yourself. For reporting, use a `budget_exchange_rates` table or fetch rates from a free API (e.g., exchangerate.host).

If you frequently move money between NOK and THB (Thailand), support multi-currency transfers: "Transferred 5000 NOK → 15000 THB" as a single logical transaction.

## Backend: `internal/budget/`

### Package Structure

- **models.go** — Account, Category, Transaction, Recurring, BudgetLimit structs
- **storage.go** — CRUD for all tables
- **recurring.go** — Auto-generate transactions from recurring items (run daily or on page load)
- **reports.go** — Category spending summaries, budget vs actual, trends
- **handlers.go** — HTTP handlers for all endpoints
- **import.go** — CSV import (for migrating from Google Sheets)

### API Endpoints

```
GET    /api/budget/accounts              — list accounts with balances
POST   /api/budget/accounts              — create account
PUT    /api/budget/accounts/{id}         — update account
DELETE /api/budget/accounts/{id}         — archive account

GET    /api/budget/categories            — list categories (grouped)
POST   /api/budget/categories            — create category
PUT    /api/budget/categories/{id}       — update category

GET    /api/budget/transactions?month=2026-03&category=5  — list/filter transactions
POST   /api/budget/transactions          — create transaction
PUT    /api/budget/transactions/{id}     — update transaction
DELETE /api/budget/transactions/{id}     — delete transaction

GET    /api/budget/recurring             — list recurring items
POST   /api/budget/recurring             — create recurring
PUT    /api/budget/recurring/{id}        — update recurring
DELETE /api/budget/recurring/{id}        — deactivate recurring

GET    /api/budget/limits?month=2026-03  — get budget limits for period
PUT    /api/budget/limits                — set/update budget limits

GET    /api/budget/summary?month=2026-03 — monthly summary (category totals vs budget)
GET    /api/budget/trends?months=6       — spending trends over time

POST   /api/budget/import/csv            — import transactions from CSV
```

## Frontend

### BudgetPage.tsx — Monthly Overview (Main View)

**Layout:**
- Month selector at top (← March 2026 →)
- **Income vs Expenses bar** — total income, total expenses, net
- **Category breakdown** — progress bars showing spent/budgeted per category
  - Green: under budget, Yellow: 80-100%, Red: over budget
- **Recent transactions** list below

### BudgetTransactions.tsx — Transaction List

- Filterable by: category, account, date range, search text
- Inline "quick add" row at top
- Each row: date, description, category badge, amount (red/green), account
- Swipe to delete on mobile

### BudgetAccounts.tsx — Account Overview

- Card per account showing balance and recent activity
- "Transfer between accounts" action
- Account settings (name, currency, icon)

### BudgetRecurring.tsx — Recurring Items

- List of all recurring transactions with next occurrence date
- Toggle active/inactive
- Edit frequency, amount, category

### BudgetCharts.tsx — Trends & Reports

- Pie chart: spending by category (current month)
- Bar chart: monthly spending over last 6-12 months
- Line chart: net worth over time (sum of all account balances)
- Use Recharts (already in Hytte dependencies)

## Migration from Google Sheets

**Recommendation**: Build a CSV import first. Export your Google Sheet as CSV, then import:

1. Export transactions tab from Google Sheets as CSV
2. `POST /api/budget/import/csv` with column mapping
3. Review imported transactions, fix categories
4. Set up recurring items to match your sheet's formulas

Alternatively, if your sheet is heavily formula-based (calculated columns), we could use the Google Sheets API to read it directly — but CSV import is simpler and doesn't create an ongoing dependency.

## Phase Plan

### Phase 1: Foundation
- Data model (accounts, categories, transactions)
- CRUD endpoints for all entities
- Basic transaction list page with month filter
- Manual transaction entry
- CSV import for migration

### Phase 2: Budgeting
- Budget limits per category per month
- Monthly summary with budget vs actual
- Category progress bars
- Over-budget alerts

### Phase 3: Recurring & Automation
- Recurring transaction definitions
- Auto-generation of recurring transactions
- Recurring management UI

### Phase 4: Reporting & Trends
- Charts (pie, bar, line)
- Multi-month trends
- Net worth tracking
- Year-over-year comparison

### Phase 5: Advanced
- Multi-currency with exchange rates
- Split transactions (one payment across multiple categories)
- Receipt photo attachment (if we want to get fancy)
- Budget templates (copy last month's budget)
- Shared family budget view

## Privacy & Security

- All financial data encrypted at rest (AES-256-GCM)
- User-scoped — no cross-user access
- Feature-gated (`budget` feature flag)
- No third-party services (all data stays in Hytte's SQLite)
- CSV import happens server-side, file not persisted

## Decisions

1. **Google Sheet review**: Pending — will share structure with sensitive data redacted (screenshot of headers/layout/formula patterns, not actual numbers). Specific reports to replicate depend on this review.
2. **Tracking scope**: Per-person AND household. Support family members — spouse (via family_links) and children. Each person can have their own spending tracked against shared or individual budgets.
3. **Budget period**: Monthly.
4. **Currency**: NOK only.
5. **Visibility**: Managed via existing admin/feature flag system.
6. **Reports**: To be determined after sheet structure review.

## Still Needed

- Review of Google Sheet structure to finalize categories, report views, and calculated fields. Share a redacted screenshot (headers, column layout, formula patterns — blur/remove actual numbers) or describe the tab layout verbally.