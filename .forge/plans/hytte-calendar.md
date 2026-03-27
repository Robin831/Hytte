# Hytte Calendar — Google Calendar Integration

**Status**: Planning
**Date**: 2026-03-26

---

## Overview

Replace the placeholder CalendarPage with a real Google Calendar integration. Since Hytte already uses Google OAuth, this is mostly about requesting additional scopes and building the UI.

## Why This Is Easier Than It Looks

- Google OAuth is already wired up (`internal/auth/config.go`)
- Session management is solid (30-day sessions, cookie-based)
- The page, route, nav item, feature flag, and i18n keys all already exist
- We just need to add calendar scopes and persist the OAuth token

## What Needs to Change

### 1. OAuth Scope Expansion

Add `https://www.googleapis.com/auth/calendar.readonly` to the scopes in `auth/config.go`. This is the minimum — read-only access to the user's calendars.

**Important**: Existing users will need to re-consent because the scope changed. On next login, Google will show the new permission. Consider showing a banner: "We've added calendar support — please log in again to enable it."

**Recommendation**: Also request `https://www.googleapis.com/auth/calendar.events` (read/write) from the start. Even if v1 is read-only, you'll want to create events later (bookings, reminders) without forcing another re-consent.

### 2. Token Persistence

Currently the OAuth token is used once during callback to fetch user info, then discarded. For Calendar API, we need to store the full token (access_token + refresh_token) so we can make API calls on behalf of the user later.

Add a `google_tokens` table:
```sql
CREATE TABLE IF NOT EXISTS google_tokens (
    user_id       INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    access_token  TEXT NOT NULL,     -- encrypted at rest
    refresh_token TEXT NOT NULL,     -- encrypted at rest
    token_type    TEXT NOT NULL DEFAULT 'Bearer',
    expiry        TEXT NOT NULL,
    scopes        TEXT NOT NULL DEFAULT '',
    updated_at    TEXT NOT NULL DEFAULT ''
);
```

Encrypt both tokens using the existing AES-256-GCM encryption (`internal/encryption`).

**Recommendation**: Request `access_type=offline` in the OAuth URL to get a refresh token. Without this, the token expires after 1 hour and the user has to log in again. Add `prompt=consent` on the first request to force the consent screen (needed for refresh token).

### 3. Backend: Calendar API Package

Create `internal/calendar/` with:

- **client.go** — Google Calendar API client, token refresh logic
- **handlers.go** — HTTP handlers:
  - `GET /api/calendar/events?start=2026-03-01&end=2026-03-31` — fetch events for date range
  - `GET /api/calendar/calendars` — list user's calendars (they may have multiple)
  - `PUT /api/calendar/settings` — save which calendars to show (store in user_preferences)
- **models.go** — Event struct (title, start, end, location, description, calendar color, all-day flag)

**Recommendation**: Use Google's Go client library `google.golang.org/api/calendar/v3` rather than raw HTTP. It handles pagination, error codes, and token refresh automatically.

### 4. Frontend: CalendarPage.tsx

Replace the stub with a real calendar view.

**View modes:**
- **Month view** (default) — grid of days with event dots/pills
- **Week view** — hourly slots with event blocks
- **Day view** — detailed hourly view
- **Agenda view** — scrollable list of upcoming events (most useful on mobile)

**Recommendation**: Don't build a calendar grid from scratch — use a library. Options:
- `@schedule-x/react` — modern, dark-theme-friendly, lightweight
- `react-big-calendar` — mature but needs styling work for dark theme
- Or: start with just the **agenda view** (a simple list) for v1. It's the most useful view and trivially easy to build. Add grid views in v2.

**Calendar selector**: Let users toggle which Google calendars are visible (most people have 3-5: personal, work, birthdays, holidays, shared family calendar).

### 5. Caching & Sync

Don't hit Google's API on every page load. Cache events in SQLite:

```sql
CREATE TABLE IF NOT EXISTS calendar_events (
    id            TEXT PRIMARY KEY,   -- Google event ID
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    calendar_id   TEXT NOT NULL,
    title         TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    location      TEXT NOT NULL DEFAULT '',
    start_time    TEXT NOT NULL,
    end_time      TEXT NOT NULL,
    all_day       INTEGER NOT NULL DEFAULT 0,
    color         TEXT NOT NULL DEFAULT '',
    updated_at    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_calendar_events_user_time
    ON calendar_events(user_id, start_time, end_time);
```

Sync strategy:
- On page load: serve from cache, trigger background sync
- Background sync: fetch events from Google with `updatedMin` parameter (incremental)
- Full sync: daily, or on user request ("Refresh" button)

### 6. i18n

Calendar keys already exist in all 3 locales. Extend with:
- `calendar.weekView`, `calendar.monthView`, `calendar.dayView`, `calendar.agenda`
- `calendar.today`, `calendar.noEvents`, `calendar.allDay`
- `calendar.selectCalendars`, `calendar.refresh`

Use `Intl.DateTimeFormat` with `i18n.language` for date formatting — Norwegian dates should show "mandag 26. mars", not "Monday March 26".

## Phase Plan

### Phase 1: Read-only agenda (simplest useful thing)
- Expand OAuth scopes + persist tokens
- Backend: fetch events from Google Calendar API
- Frontend: agenda view (list of upcoming events)
- Calendar selector (toggle which calendars to show)

### Phase 2: Calendar grid views
- Month view with event dots
- Week view with time slots
- Day view with detailed blocks
- Drag-to-navigate between views

### Phase 3: Write support (bookings)
- Create events from Hytte
- "Book" concept — family members can reserve time slots
- Recurring events support
- Push notifications for upcoming events

## Privacy & Security

- Tokens encrypted at rest (existing AES-256-GCM)
- Calendar data is user-scoped — no cross-user access
- Refresh tokens must be revocable (add a "Disconnect Calendar" button in settings)
- Consider: do family members (kids) need calendar access? Probably not in v1.

## Decisions

1. **Shared family calendar**: Yes — support shared Google Calendars so the whole family can see the same view.
2. **Bookings**: Deferred to a later phase. Focus on read-only calendar display first.
3. **Pre-selected calendars**: To be decided after initial setup — get it working, then configure defaults.
