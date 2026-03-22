# Hytte Kids Star/Reward System — Feature Plan

> "Why did the fitness tracker break up with the lazy kid? Because it couldn't keep up with all the *excuses*!" -- Motivation through terrible humor, the Hytte way.

## 1. Overview

A gamification layer for Hytte that lets parents (admins or designated "family admins") link child accounts, monitor their workout activity, and motivate them through a star-based reward economy. Kids earn stars by exercising, hitting milestones, and maintaining streaks. Stars can be spent in a parent-configured "reward shop" for real-life prizes.

Target audience: kids aged 8-15, with parent oversight.

---

## 2. Tech Stack Context (Existing)

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.26, Chi v5 router, SQLite (WAL mode, modernc.org/sqlite) |
| Frontend | React 19, TypeScript 5.9, Vite 8, Tailwind CSS v4, Recharts, Lucide icons |
| Auth | Google OAuth2, session cookies (SHA-256 hashed), `is_admin` flag |
| Notifications | Web Push (VAPID/AES-128-GCM) via `internal/push` |
| Feature gating | `user_features` table, `RequireFeature` middleware |
| Encryption | AES-256-GCM for sensitive fields at rest |
| Workouts | .FIT file import, sport/sub_sport, HR, distance, duration, pace, cadence, calories, ascent, laps, samples, tags |

---

## 3. Data Model Changes

### 3.1 New Tables

```sql
-- Parent-child account linking
CREATE TABLE IF NOT EXISTS family_links (
    id          INTEGER PRIMARY KEY,
    parent_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    child_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    nickname    TEXT NOT NULL DEFAULT '',       -- parent-chosen display name for the child
    avatar_emoji TEXT NOT NULL DEFAULT '⭐',   -- fun emoji avatar for the child's profile
    created_at  TEXT NOT NULL DEFAULT '',
    UNIQUE(parent_id, child_id)
);

CREATE INDEX IF NOT EXISTS idx_family_links_parent ON family_links(parent_id);
CREATE INDEX IF NOT EXISTS idx_family_links_child ON family_links(child_id);

-- Star transactions (immutable ledger — stars earned and spent)
CREATE TABLE IF NOT EXISTS star_transactions (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount       INTEGER NOT NULL,              -- positive = earned, negative = spent
    reason       TEXT NOT NULL,                 -- e.g. "distance_milestone", "weekly_streak", "reward_purchase"
    description  TEXT NOT NULL DEFAULT '',       -- human-readable: "Ran 5km! 🏃"
    reference_id INTEGER,                       -- workout_id, reward_id, or challenge_id depending on reason
    created_at   TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_star_transactions_user ON star_transactions(user_id, created_at);

-- Star balance cache (denormalized for fast reads)
CREATE TABLE IF NOT EXISTS star_balances (
    user_id       INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    total_earned  INTEGER NOT NULL DEFAULT 0,
    total_spent   INTEGER NOT NULL DEFAULT 0,
    current_balance INTEGER GENERATED ALWAYS AS (total_earned - total_spent) STORED
);

-- Reward shop items (parent-configured)
CREATE TABLE IF NOT EXISTS rewards (
    id          INTEGER PRIMARY KEY,
    parent_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,                  -- encrypted at rest
    description TEXT NOT NULL DEFAULT '',        -- encrypted at rest
    star_cost   INTEGER NOT NULL,
    icon_emoji  TEXT NOT NULL DEFAULT '🎁',
    is_active   INTEGER NOT NULL DEFAULT 1,
    max_claims  INTEGER NOT NULL DEFAULT 0,     -- 0 = unlimited
    created_at  TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_rewards_parent ON rewards(parent_id);

-- Reward claims (kid "buys" a reward)
CREATE TABLE IF NOT EXISTS reward_claims (
    id          INTEGER PRIMARY KEY,
    reward_id   INTEGER NOT NULL REFERENCES rewards(id) ON DELETE CASCADE,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    star_cost   INTEGER NOT NULL,               -- snapshot of cost at claim time
    status      TEXT NOT NULL DEFAULT 'pending', -- pending, approved, fulfilled, denied
    claimed_at  TEXT NOT NULL DEFAULT '',
    resolved_at TEXT NOT NULL DEFAULT '',
    parent_note TEXT NOT NULL DEFAULT ''         -- encrypted; parent can add a note
);

CREATE INDEX IF NOT EXISTS idx_reward_claims_user ON reward_claims(user_id);
CREATE INDEX IF NOT EXISTS idx_reward_claims_reward ON reward_claims(reward_id);

-- Badges / achievements
CREATE TABLE IF NOT EXISTS badges (
    id          INTEGER PRIMARY KEY,
    key         TEXT UNIQUE NOT NULL,           -- e.g. "first_workout", "streak_7", "distance_marathon"
    name        TEXT NOT NULL,
    description TEXT NOT NULL,
    icon_emoji  TEXT NOT NULL DEFAULT '🏅',
    category    TEXT NOT NULL DEFAULT 'general', -- general, distance, consistency, speed, variety, social
    tier        TEXT NOT NULL DEFAULT 'bronze'   -- bronze, silver, gold, diamond
);

-- User badge awards
CREATE TABLE IF NOT EXISTS user_badges (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    badge_id   INTEGER NOT NULL REFERENCES badges(id) ON DELETE CASCADE,
    awarded_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (user_id, badge_id)
);

-- Challenges (parent-created or system-generated)
CREATE TABLE IF NOT EXISTS challenges (
    id           INTEGER PRIMARY KEY,
    created_by   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title        TEXT NOT NULL,                 -- encrypted
    description  TEXT NOT NULL DEFAULT '',       -- encrypted
    challenge_type TEXT NOT NULL,               -- distance, duration, workout_count, streak, custom
    target_value REAL NOT NULL DEFAULT 0,       -- e.g. 10000 (meters), 5 (workouts), 7 (days)
    star_reward  INTEGER NOT NULL DEFAULT 0,
    start_date   TEXT NOT NULL DEFAULT '',
    end_date     TEXT NOT NULL DEFAULT '',
    is_active    INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT NOT NULL DEFAULT ''
);

-- Challenge participants and progress
CREATE TABLE IF NOT EXISTS challenge_participants (
    challenge_id   INTEGER NOT NULL REFERENCES challenges(id) ON DELETE CASCADE,
    user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    current_value  REAL NOT NULL DEFAULT 0,
    completed      INTEGER NOT NULL DEFAULT 0,
    completed_at   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (challenge_id, user_id)
);

-- Streak tracking
CREATE TABLE IF NOT EXISTS streaks (
    user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    streak_type     TEXT NOT NULL,              -- "daily_workout", "weekly_workout"
    current_count   INTEGER NOT NULL DEFAULT 0,
    longest_count   INTEGER NOT NULL DEFAULT 0,
    last_activity   TEXT NOT NULL DEFAULT '',   -- date of last qualifying activity
    PRIMARY KEY (user_id, streak_type)
);

-- Level / XP tracking
CREATE TABLE IF NOT EXISTS user_levels (
    user_id     INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    xp          INTEGER NOT NULL DEFAULT 0,
    level       INTEGER NOT NULL DEFAULT 1,
    title       TEXT NOT NULL DEFAULT 'Rookie Runner'
);
```

### 3.2 Existing Table Changes

```sql
-- Add family_role to users (no column needed — derived from family_links)
-- No changes to the users table itself; parent/child is a relationship, not a role.
```

### 3.3 Feature Gate

Add `"kids_stars"` to the feature gating system. The feature is enabled per-user and controls access to:
- Parent: family management, reward shop config, child stats dashboard
- Child: star balance, reward shop browsing, badge collection, challenge participation

---

## 4. Star Awarding Criteria

Stars are awarded automatically when workouts are uploaded/synced. The engine evaluates each workout against multiple criteria and sums the awards.

### 4.1 Per-Workout Base Stars

| Criterion | Stars | Description |
|-----------|-------|-------------|
| **Showed Up** | 2 | Any completed workout of at least 10 minutes earns base stars. "Half the battle is showing up!" |
| **Duration Bonus** | +1 per 15 min | Longer workouts earn more (cap at +8 for 2 hours) |
| **Effort Bonus** | +1 to +3 | Based on average HR zone: Zone 2 = +1, Zone 3 = +2, Zone 4+ = +3 |

### 4.2 Distance Milestones

Stars awarded when cumulative or single-workout distance thresholds are crossed.

| Milestone | Stars | Trigger |
|-----------|-------|---------|
| **First Kilometer** | 5 | First workout with distance >= 1 km |
| **5K Finisher** | 10 | Single workout >= 5 km |
| **10K Hero** | 20 | Single workout >= 10 km |
| **Half Marathon Legend** | 50 | Single workout >= 21.1 km |
| **Century Club** | 25 | Cumulative 100 km total |
| **500K Explorer** | 75 | Cumulative 500 km total |
| **1000K Titan** | 150 | Cumulative 1000 km total |

### 4.3 Consistency Stars

| Criterion | Stars | Description |
|-----------|-------|-------------|
| **3-Day Streak** | 5 | Worked out 3 days in a row |
| **7-Day Warrior** | 15 | Full week streak |
| **14-Day Legend** | 35 | Two-week streak |
| **30-Day Ironkid** | 100 | Month-long streak (!) |
| **Weekend Warrior** | 5 | Worked out on both Saturday and Sunday |
| **Early Bird** | 3 | Workout started before 8:00 AM |
| **Night Owl** | 3 | Workout started after 8:00 PM |

### 4.4 Personal Records

| Record Type | Stars | Description |
|-------------|-------|-------------|
| **Fastest 5K** | 10 | New personal best 5K time |
| **Longest Run** | 10 | New personal best distance |
| **Highest Calorie Burn** | 8 | New single-workout calorie record |
| **Fastest Pace** | 10 | New best avg pace for any workout > 2 km |
| **Most Elevation** | 8 | New single-workout ascent record |

### 4.5 Heart Rate Zone Training

| Criterion | Stars | Description |
|-----------|-------|-------------|
| **Zone Commander** | 5 | Spent 80%+ of workout in a single target zone |
| **Zone Explorer** | 8 | Hit all 5 HR zones in one workout |
| **Easy Day Hero** | 3 | Entire workout in Zone 1-2 (recovery day discipline!) |
| **Threshold Trainer** | 5 | 20+ minutes in Zone 4 |

HR zones are calculated from estimated max HR (220 - age) or from user preferences if set. Zone boundaries: Z1 (50-60%), Z2 (60-70%), Z3 (70-80%), Z4 (80-90%), Z5 (90-100%).

### 4.6 Activity Variety

| Criterion | Stars | Description |
|-----------|-------|-------------|
| **Try Something New** | 5 | First workout in a new sport type |
| **All-Rounder** | 15 | Logged 3+ different sport types in a single week |
| **Indoor/Outdoor Mix** | 5 | Did both indoor and outdoor workouts in the same week |
| **Cross-Training Champion** | 20 | 5+ different sport types logged all-time |

### 4.7 Fun / Social Stars

| Criterion | Stars | Description |
|-----------|-------|-------------|
| **Family Workout** | 10 | Two or more family members logged a workout on the same day |
| **High-Five Friday** | 5 | Any workout completed on a Friday |
| **Holiday Hero** | 10 | Workout on a national holiday |
| **Birthday Burn** | 15 | Workout on your birthday (if birthday is set in preferences) |

---

## 5. Weekly Bonus System

Evaluated every Sunday at midnight (or on Monday's first login). Bonuses stack with per-workout stars.

| Weekly Bonus | Stars | Condition |
|--------------|-------|-----------|
| **Week Complete** | 10 | At least 3 workouts this week |
| **Active Every Day** | 25 | Logged a workout every day of the week |
| **Distance Goal** | 15 | Hit weekly distance target (parent-configurable, default 15 km) |
| **Duration Goal** | 15 | Hit weekly duration target (parent-configurable, default 3 hours) |
| **Improvement Bonus** | 10 | Total weekly distance or duration improved over the previous week |
| **Streak Multiplier** | x1.5 | If on a 2+ week streak of hitting the "Week Complete" bonus, multiply all weekly bonus stars by 1.5 |
| **Perfect Week** | 50 | All of the above conditions met simultaneously |

### Weekly Configuration (Parent Settings)

Parents can customize weekly targets per child:
- Weekly distance target (km)
- Weekly duration target (hours)
- Minimum workouts per week for "Week Complete"
- Custom bonus star amounts

---

## 6. Reward / Gift Shop System

### 6.1 Parent Configuration

Parents manage a reward catalog through the parent dashboard:

```typescript
interface Reward {
  id: number
  title: string           // "Extra 30 min screen time"
  description: string     // "Redeem for 30 minutes of extra gaming or TV"
  star_cost: number       // 50
  icon_emoji: string      // "🎮"
  is_active: boolean
  max_claims: number      // 0 = unlimited, or set a cap
}
```

**Suggested Default Rewards** (parent can edit/remove):

| Reward | Suggested Cost | Emoji |
|--------|---------------|-------|
| Extra 30 min screen time | 50 | 🎮 |
| Choose dinner tonight | 75 | 🍕 |
| Skip one chore | 100 | 🧹 |
| Movie night pick | 80 | 🎬 |
| Ice cream outing | 120 | 🍦 |
| New book | 150 | 📚 |
| Stay up 30 min later | 60 | 🌙 |
| Friend sleepover | 300 | 🏠 |
| Special outing (parent's choice) | 500 | 🎢 |
| $5 spending money | 200 | 💵 |

### 6.2 Claim Flow

1. Kid browses available rewards and clicks "Claim" on one they can afford.
2. Stars are deducted immediately (prevents double-spending).
3. Claim appears in parent's "Pending Claims" queue with status `pending`.
4. Parent reviews and marks as `approved` → `fulfilled` (or `denied` with a note, stars refunded).
5. Push notification sent to parent on claim, to child on approval/denial.

### 6.3 API Endpoints

```
GET    /api/stars/balance                    — current star balance + recent transactions
GET    /api/stars/transactions?limit=50      — paginated transaction history
GET    /api/stars/rewards                    — available rewards (active, affordable)
POST   /api/stars/rewards/{id}/claim         — claim a reward
GET    /api/stars/claims                     — user's claim history
GET    /api/family/children                  — parent: list linked children with stats
POST   /api/family/children                  — parent: link a child by email/invite code
DELETE /api/family/children/{id}             — parent: unlink a child
GET    /api/family/children/{id}/stats       — parent: child's workout summary + stars
GET    /api/family/children/{id}/workouts    — parent: child's recent workouts (general stats only)
GET    /api/family/rewards                   — parent: manage reward catalog
POST   /api/family/rewards                   — parent: create reward
PUT    /api/family/rewards/{id}              — parent: update reward
DELETE /api/family/rewards/{id}              — parent: delete reward
GET    /api/family/claims                    — parent: pending/all claims from children
PUT    /api/family/claims/{id}               — parent: approve/deny claim
GET    /api/stars/badges                     — user's earned badges
GET    /api/stars/badges/available           — all badges with earned/unearned status
GET    /api/stars/challenges                 — active challenges
POST   /api/family/challenges               — parent: create challenge
GET    /api/stars/leaderboard                — family leaderboard
GET    /api/stars/streaks                    — user's current streaks
```

---

## 7. Gamification Beyond Stars

### 7.1 Levels & Ranks

XP is earned alongside stars (1 star = 1 XP, but XP is never spent). Levels use a curve so early levels come fast, keeping kids hooked.

| Level | XP Required | Title | Emoji |
|-------|-------------|-------|-------|
| 1 | 0 | Rookie Runner | 🐣 |
| 2 | 50 | Eager Explorer | 🐤 |
| 3 | 150 | Steady Stepper | 🚶 |
| 4 | 300 | Power Pacer | 🏃 |
| 5 | 500 | Trail Tracker | 🥾 |
| 6 | 800 | Rhythm Rider | 🚴 |
| 7 | 1200 | Iron Junior | 💪 |
| 8 | 1800 | Speed Demon | ⚡ |
| 9 | 2500 | Mountain Goat | 🐐 |
| 10 | 3500 | Legend | 🏆 |
| 11 | 5000 | Mythic Athlete | 🦅 |
| 12 | 7000 | Hytte Hero | 👑 |

Formula: `XP_for_level(n) = round(50 * n^1.6)` — gives a smooth curve.

Level-up triggers a celebratory push notification and a confetti animation on next login.

### 7.2 Badges & Achievements

Badges are one-time awards displayed on the kid's profile. Categories:

**Distance Badges:**
- First Steps (1 km total), Park Runner (5 km single), City Explorer (10 km single), Half Marathon Hero (21.1 km), Marathon Master (42.2 km)
- Cumulative: 50K Club, 100K Club, 500K Club, 1000K Club

**Consistency Badges:**
- Getting Started (3 workouts), Week Warrior (7-day streak), Fortnight Fighter (14-day streak), Monthly Monster (30-day streak), Quarterly Quad (90-day streak)
- 10 Workouts, 25 Workouts, 50 Workouts, 100 Workouts, 250 Workouts

**Speed Badges:**
- Sub-30 5K, Sub-25 5K, Sub-20 5K (for runners)
- PR Hunter (set 3 personal records), Record Breaker (set 10 personal records)

**Variety Badges:**
- Jack of All Trades (3 sport types), Renaissance Athlete (5 sport types), Ultimate All-Rounder (8 sport types)
- Indoor Warrior (10 indoor workouts), Outdoor Explorer (10 outdoor workouts)

**Heart Badges:**
- Zone Master (spend 80%+ in target zone 10 times), Heart Monitor (log 50 workouts with HR data)

**Fun Badges:**
- Early Bird (5 workouts before 8 AM), Night Owl (5 workouts after 8 PM)
- Holiday Hero (workout on 3 holidays), Weekend Regular (10 weekend workouts)
- Family Fitness (5 same-day family workouts)

**Secret Badges** (not shown until earned):
- Palindrome Day (workout on a palindrome date like 2026-06-02)
- Leap Year Logger (workout on Feb 29)
- Midnight Madness (workout that spans midnight)
- The Answer (log exactly 42.195 km in a single workout — marathon distance)

### 7.3 Family Leaderboard

A friendly weekly leaderboard among family members:

- **Weekly Stars Earned** — who earned the most this week?
- **Weekly Distance** — total km this week
- **Weekly Workout Count** — most sessions
- **Current Streak** — longest active streak

Leaderboard resets weekly to keep it competitive. All-time stats shown separately. Parent can optionally participate to model behavior (and get roasted by their kids when beaten).

### 7.4 Challenges (Parent-Created)

Parents can create time-boxed challenges for their children:

```typescript
interface Challenge {
  id: number
  title: string            // "Summer 5K Challenge"
  description: string      // "Run a total of 5km this week"
  challenge_type: string   // "distance" | "duration" | "workout_count" | "streak" | "custom"
  target_value: number     // 5000 (meters)
  star_reward: number      // 25
  start_date: string
  end_date: string
  participants: ChallengeParticipant[]
}
```

**System-Generated Challenges** (auto-created weekly):
- "Beat Last Week" — improve on last week's total distance
- "Try a New Sport" — log a workout in a sport you haven't done this month
- "Consistency King" — work out at least 4 days this week
- "Heart Zone Challenge" — spend at least 30 min in Zone 3+ this week

### 7.5 Streak Tracking with Visual Flames

Streak types tracked:
- **Daily Workout Streak** — consecutive days with at least one workout (any duration > 10 min)
- **Weekly Workout Streak** — consecutive weeks with at least N workouts (parent-configurable, default 3)

Visual representation:
- 1-2 days: small flame 🔥
- 3-6 days: medium flame (animated flicker)
- 7-13 days: large flame with glow effect
- 14-29 days: blue flame 🔵🔥 (rare!)
- 30+ days: rainbow/diamond flame with particle effects ✨🔥

Streak freeze: parent can grant 1 "streak shield" per week (e.g., for sick days or rest days). Costs 0 stars but prevents streak breakage. Configurable in parent settings.

---

## 8. UI Components

### 8.1 Parent Dashboard (`/family`)

A dedicated page for parents to manage the family system:

- **Children Overview** — cards for each child showing:
  - Avatar emoji + nickname
  - Current level and XP bar
  - Star balance
  - Active streak (with flame indicator)
  - This week's workout count and total distance
  - Quick link to detailed stats
- **Pending Claims** — reward claims awaiting approval (badge count in sidebar)
- **Reward Shop Management** — CRUD for rewards with drag-to-reorder
- **Challenge Management** — create/edit/view active and past challenges
- **Weekly Report** — summary of all children's activity for the past week

### 8.2 Kid Star Dashboard (`/stars`)

The primary view for children — designed to be fun and visually exciting:

- **Star Balance** — large, animated star count with sparkle effects
- **Level Progress** — XP bar showing progress to next level, with current rank title
- **Streak Display** — flame animation with day count
- **Recent Stars** — scrolling feed of recent star earnings with reasons ("You ran 5km! +10 ⭐")
- **Quick Stats** — this week's workouts, distance, time
- **Badge Showcase** — grid of earned badges (greyed-out silhouettes for unearned)
- **Active Challenges** — progress bars for current challenges
- **Reward Shop** — browsable reward list with "Claim" buttons
- **Family Leaderboard** — compact weekly standings

### 8.3 Design Notes

- Use bright, energetic colors for the kids view (gradients of yellow/orange for stars, purple/blue for levels)
- Tailwind CSS with dark theme base (consistent with existing Hytte styling) but with more color accents
- Animations: CSS keyframes for star sparkle, streak flames, level-up confetti (keep lightweight — no heavy animation libraries)
- Responsive: works well on phones (kids often check on parent's phone)
- Sound effects: optional (preference toggle) — star earn chime, level-up fanfare (Web Audio API, tiny MP3s)

### 8.4 New Frontend Pages

| Page | Route | Description |
|------|-------|-------------|
| `Stars.tsx` | `/stars` | Kid's star dashboard |
| `StarRewards.tsx` | `/stars/rewards` | Reward shop browsing |
| `StarBadges.tsx` | `/stars/badges` | Badge collection |
| `StarChallenges.tsx` | `/stars/challenges` | Active challenges |
| `Family.tsx` | `/family` | Parent family dashboard |
| `FamilyChildDetail.tsx` | `/family/children/:id` | Detailed child stats |
| `FamilyRewards.tsx` | `/family/rewards` | Reward shop management |
| `FamilyChallenges.tsx` | `/family/challenges` | Challenge management |

### 8.5 Sidebar Updates

Add to `Sidebar.tsx`:
- "Stars" icon (Lucide `Star`) — visible to users who are linked as children
- "Family" icon (Lucide `Users`) — visible to users who have linked children
- Claim notification badge on "Family" when pending claims exist

---

## 9. Notifications

Using the existing Web Push infrastructure (`internal/push`):

| Event | Recipient | Message |
|-------|-----------|---------|
| Stars earned (per workout) | Child | "You earned 15 ⭐ from your run! Total: 243 ⭐" |
| Level up | Child | "LEVEL UP! You're now a Power Pacer (Level 4)! 🎉" |
| New badge earned | Child | "New badge unlocked: 🏅 Week Warrior!" |
| Streak milestone | Child | "🔥 7-day streak! Keep it burning!" |
| Streak about to break | Child | "Your 5-day streak will break tomorrow if you don't work out! 🔥⚠️" |
| Close to a reward | Child | "You're only 12 ⭐ away from 'Ice cream outing'! 🍦" |
| Reward claimed | Parent | "Emma claimed 'Extra screen time' (50 ⭐). Approve?" |
| Reward approved | Child | "Your reward 'Extra screen time' was approved! 🎉" |
| Reward denied | Child | "Your reward claim was denied. Stars have been refunded." |
| Weekly summary | Parent | "This week: Emma earned 85 ⭐, ran 12 km. Oliver earned 60 ⭐, ran 8 km." |
| Challenge completed | Parent + Child | "Emma completed 'Summer 5K Challenge'! +25 ⭐" |
| Challenge expiring | Child | "Only 2 days left in 'Beat Last Week'! You need 3 more km." |
| Family workout | All family | "Family workout day! Both Emma and Oliver worked out today! +10 ⭐ each" |

### Notification Timing

- Per-workout notifications: sent immediately after workout upload processing completes
- Streak warnings: sent at 7 PM if no workout logged that day and streak is at risk
- Weekly summaries: sent Monday morning at 8 AM
- Challenge reminders: sent 2 days before expiry and on final day
- Respect existing quiet hours system (`internal/quiethours`)

---

## 10. Backend Package Structure

```
internal/
├── family/
│   ├── models.go           # FamilyLink, StarTransaction, Reward, RewardClaim, Badge, etc.
│   ├── storage.go          # CRUD for family_links, rewards, claims
│   ├── handlers.go         # Parent API handlers (children, rewards, claims, challenges)
│   ├── handlers_test.go
│   ├── storage_test.go
│   └── invite.go           # Invite code generation and validation
├── stars/
│   ├── engine.go           # Star calculation engine — evaluates workout against all criteria
│   ├── engine_test.go
│   ├── milestones.go       # Distance milestones, personal records detection
│   ├── milestones_test.go
│   ├── streaks.go          # Streak tracking and maintenance
│   ├── streaks_test.go
│   ├── badges.go           # Badge definitions and award logic
│   ├── badges_test.go
│   ├── weekly.go           # Weekly bonus evaluation
│   ├── weekly_test.go
│   ├── levels.go           # XP/level calculation
│   ├── levels_test.go
│   ├── leaderboard.go      # Family leaderboard queries
│   ├── challenges.go       # Challenge progress tracking
│   ├── handlers.go         # Kid-facing API handlers (balance, transactions, rewards, badges)
│   ├── handlers_test.go
│   └── notifications.go    # Star-related push notification logic
```

---

## 11. Implementation Phases

### Phase 1: Foundation (1-2 weeks)

**Goal:** Parent-child linking and basic star earning.

- [ ] Add `kids_stars` feature flag to feature gating system
- [ ] Create all new database tables in `db.go:createSchema()`
- [ ] Implement `internal/family` package: CRUD for family links
- [ ] Implement invite-code flow for linking children (parent generates code, child enters it)
- [ ] Implement `internal/stars/engine.go`: base star calculation (showed up + duration + effort)
- [ ] Hook star engine into workout upload pipeline (after FIT parse, calculate and award stars)
- [ ] Create `GET /api/stars/balance` and `GET /api/stars/transactions` endpoints
- [ ] Create `GET /api/family/children` and child management endpoints
- [ ] Frontend: basic `Stars.tsx` page with balance display
- [ ] Frontend: basic `Family.tsx` page with child linking
- [ ] Add sidebar entries with feature-gate visibility

### Phase 2: Milestones & Rewards (1-2 weeks)

**Goal:** Distance milestones, the reward shop, and claim flow.

- [ ] Implement milestone detection (distance, personal records)
- [ ] Implement reward CRUD for parents
- [ ] Implement claim flow (claim, approve/deny, refund)
- [ ] Push notifications for claims (to parent) and approvals (to child)
- [ ] Frontend: `StarRewards.tsx` — reward browsing and claiming
- [ ] Frontend: `FamilyRewards.tsx` — reward management for parents
- [ ] Frontend: pending claims badge in sidebar

### Phase 3: Streaks & Consistency (1 week)

**Goal:** Streak tracking, consistency stars, and weekly bonuses.

- [ ] Implement streak tracking (daily and weekly)
- [ ] Implement consistency star criteria
- [ ] Implement weekly bonus evaluation (cron-like or on-login)
- [ ] Streak warning notifications
- [ ] Streak shield (parent-granted freeze)
- [ ] Frontend: streak flame visualization
- [ ] Frontend: weekly bonus summary

### Phase 4: Badges & Levels (1 week)

**Goal:** Achievement system and leveling.

- [ ] Seed badge definitions in database
- [ ] Implement badge award logic (triggered per-workout and on milestone)
- [ ] Implement XP/level system
- [ ] Level-up notifications
- [ ] Badge-earned notifications
- [ ] Frontend: `StarBadges.tsx` — badge collection with earned/locked display
- [ ] Frontend: level progress bar and rank display on Stars dashboard
- [ ] Frontend: confetti animation on level-up

### Phase 5: Challenges & Leaderboard (1 week)

**Goal:** Social/competitive features.

- [ ] Implement challenge CRUD for parents
- [ ] Implement system-generated weekly challenges
- [ ] Implement challenge progress tracking (updated per-workout)
- [ ] Implement family leaderboard queries
- [ ] Challenge notifications (completed, expiring)
- [ ] Frontend: `StarChallenges.tsx` — challenge participation
- [ ] Frontend: `FamilyChallenges.tsx` — challenge management
- [ ] Frontend: leaderboard component on Stars dashboard

### Phase 6: Polish & Delight (1 week)

**Goal:** The details that make kids love it.

- [ ] Parent dashboard weekly report view
- [ ] Child detail stats page for parents (`FamilyChildDetail.tsx`)
- [ ] Activity variety star criteria
- [ ] Fun/social star criteria (family workout, holidays, etc.)
- [ ] Sound effects (optional, preference-toggled)
- [ ] Animation polish (sparkles, flame flicker, confetti)
- [ ] Secret badges
- [ ] Weekly summary push notification to parents
- [ ] "Close to a reward" notification logic
- [ ] Parent-configurable weekly targets per child
- [ ] Mobile responsiveness pass

---

## 12. Additional Creative Ideas

### 12.1 Avatar Builder
Instead of just an emoji, let kids build a simple pixel-art avatar that "evolves" as they level up. At level 1 it's a small character; at level 5 it gets armor; at level 10 it has wings. SVG-based, lightweight, stored as a JSON config.

### 12.2 Workout Story Mode
Frame workouts as adventures: "You ran 3.2 km today — that's like running from the Shire to Bree! You're 15% of the way to Rivendell!" Track cumulative distance as progress along a fictional journey (configurable theme: Lord of the Rings, space exploration, pirate treasure map). One more reason to put in the *miles* — get it? Because they're literally going the distance? I'll see myself out.

### 12.3 Star Jar Visualization
Instead of just a number, show stars filling up a visual jar/container. When the jar is full (e.g., every 100 stars), it "breaks" with an animation and the kid gets a bonus. The jar can be themed (space, underwater, forest).

### 12.4 Rest Day Wisdom
When a kid takes a rest day (and doesn't break their streak thanks to a shield), show a fun fitness fact: "Did you know? Your muscles actually grow while you rest, not while you exercise!" Encourages healthy recovery habits.

### 12.5 Seasonal Events
Auto-generated limited-time challenges tied to seasons/holidays:
- **Summer Sprint** (June-Aug): extra stars for outdoor workouts
- **Winter Warrior** (Dec-Feb): bonus for cold-weather workouts
- **Spooky Sprint** (October): Halloween-themed badges
- **New Year's Resolution** (January): kickstart challenge

### 12.6 Photo Proof (Optional)
Kids can optionally attach a selfie or photo to a workout (stored encrypted). Parents see these in the child detail view. Great for outdoor adventures. Not required for stars — purely for fun sharing.

### 12.7 Workout Dedication
Kids can dedicate a workout to someone: "This run is for Grandma!" Shows up in the family feed. Small bonus star for dedications (max 1/day to prevent spam).

### 12.8 "Beat My Parent" Mode
If a parent also tracks workouts, kids can challenge them directly. If a kid beats the parent's weekly distance (scaled by age factor), they get a massive bonus. Nothing motivates a kid quite like *bragging rights* over mom or dad.

### 12.9 Star Savings Account
Kids can optionally put stars in "savings" where they earn 10% interest per week (compounding!). Teaches delayed gratification and basic financial concepts. Stars in savings can't be spent until withdrawn (1-day delay). A compound interest in fitness — you might say the returns are *running* high.

### 12.10 Workout Bingo
A weekly 3x3 bingo card with challenges like "Run over 2km", "Workout before school", "Try a new sport", "Beat yesterday's distance". Complete a row/column/diagonal for bonus stars. Full card = jackpot.

---

## 13. Privacy & Safety Considerations

- Children's workout data is only visible to linked parents, never to other families
- Parents can only see general stats (distance, duration, HR averages, calories) — not GPS tracks or detailed location data
- Invite codes expire after 24 hours and can only be used once
- A child can only be linked to one parent account (prevents abuse)
- Parent can unlink a child at any time (child keeps their stars and badges)
- All user-generated text (reward names, challenge descriptions, nicknames) is encrypted at rest per Hytte conventions
- No public profiles or cross-family visibility — this is a private family feature
- Age-appropriate notifications only (no pressure, no shame)

---

## 14. Configuration (forge.yaml / parent settings)

Parent-configurable settings stored in `user_preferences`:

```yaml
# Per-child settings (stored as JSON in user_preferences)
kids_stars_weekly_distance_target_km: 15
kids_stars_weekly_duration_target_hours: 3
kids_stars_weekly_min_workouts: 3
kids_stars_streak_shields_per_week: 1
kids_stars_notifications_enabled: true
kids_stars_sound_effects: false
kids_stars_leaderboard_visible: true
```

---

## 15. Testing Strategy

- Unit tests for the star engine (each criterion tested independently with edge cases)
- Unit tests for milestone detection (cumulative, single-workout, personal records)
- Unit tests for streak logic (continuation, breakage, shield usage, timezone handling)
- Unit tests for weekly bonus calculation
- Integration tests for the claim flow (claim, approve, deny, refund, double-spend prevention)
- Integration tests for family linking (invite code generation, acceptance, unlinking)
- Frontend: manual testing with mock data for animations and responsiveness
- All storage tests use `setupTestDB()` with `:memory:` SQLite per Hytte conventions
