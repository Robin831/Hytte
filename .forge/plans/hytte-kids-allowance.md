# Hytte Kids Allowance — Chores, Earnings & Weekly Pay

**Status**: Planning
**Date**: 2026-03-26

---

## Overview

A gamified chore and allowance system for kids — similar to the Stars system but with real money. Parents define chores with NOK values, kids mark them as done, parents approve, and at the end of the week Hytte calculates what each kid earned.

## How It Relates to Stars

The Stars system rewards *exercise*. This system rewards *responsibility*. They share a lot of UX DNA:

| Aspect | Stars | Allowance |
|--------|-------|-----------|
| Currency | Stars (virtual) | NOK (real money) |
| Earning | Automatic (workout detected) | Manual (kid claims, parent approves) |
| Bonuses | Milestones, streaks, badges | Bonuses, multipliers, extra tasks |
| Spending | Reward shop (virtual) | Real purchases (parent pays out) |
| Target user | Kids | Kids + Parents |

**Recommendation**: Build this as a separate feature (`kids_allowance`) but reuse the family_links infrastructure from Stars. The parent-child relationship, feature gating, and notification patterns are identical. Consider sharing UI components (approval flows, streak displays) where it makes sense.

## Core Concepts

### Chores

A chore is a recurring task with a fixed value:

- **Name**: "Rydde rommet" (clean room)
- **Value**: 20 NOK
- **Frequency**: daily, weekly, or one-time
- **Assigned to**: specific child or "any child"
- **Verification**: parent approval required (default) or auto-approved
- **Icon**: emoji picked by parent

### Extras

One-off tasks that aren't regular chores:

- Parent posts: "Hjelp meg å vaske bilen — 50 kr"
- Any child can claim it
- Parent approves when done
- Great for odd jobs and teachable moments

### Bonuses

Automatic rewards for good patterns (mirrors Stars system):

- **Full Week**: Complete all assigned daily chores Mon-Sun → +20% bonus
- **Early Bird**: Complete chore before noon → +5 NOK
- **Streak**: X consecutive days of all chores done → escalating bonus
- **Quality Bonus**: Parent can add a discretionary tip ("Extra thorough today! +10 kr")

### Weekly Summary

Every Sunday (configurable), Hytte generates a summary:
- Base earnings from completed chores
- Bonuses earned
- Deductions (if you want to support those — see open questions)
- **Total to pay out**
- Parent gets a notification: "Payout summary: Emma earned 185 kr, Oliver earned 140 kr"

## Data Model

```sql
-- Chore definitions (parent creates these)
CREATE TABLE IF NOT EXISTS allowance_chores (
    id          INTEGER PRIMARY KEY,
    parent_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    child_id    INTEGER REFERENCES users(id) ON DELETE CASCADE,  -- null = any child
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    amount      REAL NOT NULL,           -- NOK value
    currency    TEXT NOT NULL DEFAULT 'NOK',
    frequency   TEXT NOT NULL DEFAULT 'daily',  -- daily, weekly, once
    icon        TEXT NOT NULL DEFAULT '🧹',
    requires_approval INTEGER NOT NULL DEFAULT 1,
    active      INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT ''
);

-- Chore completions (kid claims, parent approves)
CREATE TABLE IF NOT EXISTS allowance_completions (
    id          INTEGER PRIMARY KEY,
    chore_id    INTEGER NOT NULL REFERENCES allowance_chores(id) ON DELETE CASCADE,
    child_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    date        TEXT NOT NULL,            -- YYYY-MM-DD
    status      TEXT NOT NULL DEFAULT 'pending',  -- pending, approved, rejected
    approved_by INTEGER REFERENCES users(id),
    approved_at TEXT,
    notes       TEXT NOT NULL DEFAULT '',  -- kid can add a note, parent can add feedback
    created_at  TEXT NOT NULL DEFAULT '',
    UNIQUE(chore_id, child_id, date)      -- one completion per chore per day per child
);

-- Extra tasks (one-off jobs)
CREATE TABLE IF NOT EXISTS allowance_extras (
    id          INTEGER PRIMARY KEY,
    parent_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    child_id    INTEGER REFERENCES users(id),  -- null = open to any child
    name        TEXT NOT NULL,
    amount      REAL NOT NULL,
    currency    TEXT NOT NULL DEFAULT 'NOK',
    status      TEXT NOT NULL DEFAULT 'open',  -- open, claimed, completed, approved, expired
    claimed_by  INTEGER REFERENCES users(id),
    completed_at TEXT,
    approved_at  TEXT,
    expires_at   TEXT,                    -- optional deadline
    created_at   TEXT NOT NULL DEFAULT ''
);

-- Bonus rules (configurable per parent)
CREATE TABLE IF NOT EXISTS allowance_bonus_rules (
    id          INTEGER PRIMARY KEY,
    parent_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type        TEXT NOT NULL,           -- full_week, early_bird, streak, quality
    multiplier  REAL NOT NULL DEFAULT 1.0,  -- e.g., 1.2 for 20% bonus
    flat_amount REAL NOT NULL DEFAULT 0,    -- e.g., 5 NOK for early bird
    active      INTEGER NOT NULL DEFAULT 1
);

-- Weekly payout summaries
CREATE TABLE IF NOT EXISTS allowance_payouts (
    id          INTEGER PRIMARY KEY,
    parent_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    child_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    week_start  TEXT NOT NULL,           -- YYYY-MM-DD (Monday)
    base_amount REAL NOT NULL DEFAULT 0,
    bonus_amount REAL NOT NULL DEFAULT 0,
    total_amount REAL NOT NULL DEFAULT 0,
    currency    TEXT NOT NULL DEFAULT 'NOK',
    paid_out    INTEGER NOT NULL DEFAULT 0,
    paid_at     TEXT,
    created_at  TEXT NOT NULL DEFAULT ''
);
```

## Backend: `internal/allowance/`

### Package Structure

- **models.go** — Chore, Completion, Extra, BonusRule, Payout structs
- **storage.go** — CRUD operations
- **calculator.go** — Weekly earnings computation (base + bonuses)
- **handlers.go** — HTTP handlers

### API Endpoints

**Parent endpoints:**
```
GET    /api/allowance/chores              — list all chores
POST   /api/allowance/chores              — create chore
PUT    /api/allowance/chores/{id}         — update chore
DELETE /api/allowance/chores/{id}         — deactivate chore

GET    /api/allowance/pending             — list pending approvals
POST   /api/allowance/approve/{id}        — approve completion
POST   /api/allowance/reject/{id}         — reject completion (with reason)
POST   /api/allowance/quality-bonus/{id}  — add quality bonus to completion

POST   /api/allowance/extras              — create extra task
GET    /api/allowance/extras              — list extras

GET    /api/allowance/payouts?child={id}&weeks=4  — payout history
POST   /api/allowance/payouts/{id}/paid   — mark payout as paid

GET    /api/allowance/bonuses             — list bonus rules
PUT    /api/allowance/bonuses             — update bonus rules
```

**Kid endpoints:**
```
GET    /api/allowance/my/chores           — today's chores (what do I need to do?)
POST   /api/allowance/my/complete/{id}    — mark chore as done
GET    /api/allowance/my/extras           — available extra tasks
POST   /api/allowance/my/claim-extra/{id} — claim an extra task
GET    /api/allowance/my/earnings         — this week's earnings so far
GET    /api/allowance/my/history          — past weekly earnings
```

## Frontend

### Parent View: AllowancePage.tsx (`/allowance`)

**Tabs:**
1. **Today** — which kids have done what today, pending approvals (approve/reject buttons)
2. **Chores** — manage chore definitions (add, edit, assign, set values)
3. **Extras** — create and manage extra tasks
4. **Payouts** — weekly summaries, mark as paid, history chart
5. **Settings** — bonus rules, payout day, notification preferences

**Approval UX**: Swipeable cards on mobile. Each pending completion shows: kid avatar, chore name, time completed, approve ✅ / reject ❌ buttons. Optional: add quality bonus (+NOK) inline.

### Kid View: MyChoresPage.tsx (`/chores`)

**Layout (designed for kids — big, colorful, clear):**

1. **Today's Chores** — checklist style
   - Each chore: icon, name, value (e.g., "🧹 Rydde rommet — 20 kr")
   - Tap to mark done → shows "Waiting for approval" state
   - Approved → green checkmark + amount
   - Rejected → red X with parent's feedback

2. **This Week's Earnings** — running total
   - Progress bar toward a goal (if kid has set a savings goal)
   - Base + bonuses breakdown

3. **Extras Board** — available extra tasks
   - Card-style layout with "Claim" button
   - Shows deadline if set

4. **Streak Counter** — consecutive days with all chores done
   - Reuse the flame visualization from Stars system

**Recommendation**: The kid view should feel like the Stars page — bright, fun, motivating. Use the same gradient/animation patterns. Consider: "You've earned 145 kr this week! 🎉" with the same sparkle effect.

### Notifications

Reuse the existing `internal/push` infrastructure:

- **Kid**: "New extra task: Vaske bilen — 50 kr! 🚗"
- **Kid**: "Your chore 'Rydde rommet' was approved! +20 kr ✅"
- **Kid**: "Full week bonus! +20% 🔥"
- **Parent**: "Emma marked 'Rydde rommet' as done — approve?"
- **Parent**: "Weekly summary: Emma 185 kr, Oliver 140 kr — tap to review"

## Synergy with Stars

**Recommendation**: Consider a "double reward" concept — completing chores earns both money AND stars. This reinforces the habit from both angles. The chore system gives tangible reward (money), the stars system gives gamification reward (levels, badges). A "Chore Champion" badge in the Stars system could unlock when the kid completes all chores for a month.

## Phase Plan

### Phase 1: Core chore system
- Data model + storage
- Parent: create/manage chores
- Kid: view chores, mark as done
- Parent: approve/reject
- Weekly earnings calculator

### Phase 2: Extras & Bonuses
- Extra tasks (one-off jobs)
- Bonus rules (full week, streaks, early bird)
- Quality bonus (parent discretionary tip)

### Phase 3: Payout & History
- Weekly payout summary generation
- "Mark as paid" flow
- Earnings history chart
- Kid savings goal tracking

### Phase 4: Polish
- Push notifications
- Stars system integration (chore badges)
- Dashboard widget (compact view)
- Recurring chore templates

## Privacy & Security

- Feature-gated (`kids_allowance`)
- Parent-child relationship via existing family_links
- Kids can only see their own chores and earnings
- All amounts encrypted at rest
- No actual payment processing — this is a tracking/calculation tool, parent pays out manually

## Decisions

1. **No deductions** — positive reinforcement only, no penalties for missed chores.
2. **Base allowance**: Yes — a guaranteed minimum weekly amount regardless of chores completed. Chores and extras earn on top.
3. **Kids**: Three boys (7, 8, 10) plus a 2-year-old girl (not using the system yet). UX must be dead simple — big buttons, clear feedback, minimal reading.
4. **Savings goals**: Yes — "I'm saving for X, need Y more weeks" with progress tracking.
5. **Auto-approve**: Yes — unapproved completions auto-approve after 24h to prevent parent bottleneck.
6. **Currency**: NOK only.
