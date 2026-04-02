# CLAUDE.md

## Build & Run

```bash
# Build the backend
make build

# Run the backend (default port 8080)
make run

# Run with custom port
PORT=3000 go run ./cmd/server

# Run tests
make test

# Clean build artifacts
make clean

# Frontend dev server (proxies /api to :8080)
cd web && npm run dev
```

## Project Structure

```
Hytte/
├── cmd/server/main.go          # Entry point: DB init, router, hourly session cleanup
├── internal/
│   ├── api/
│   │   ├── router.go           # Chi router: API routes + SPA fallback
│   │   └── health.go           # GET /api/health
│   ├── auth/
│   │   ├── config.go           # Google OAuth2 config (env vars)
│   │   ├── handlers.go         # Google login/callback/logout
│   │   ├── middleware.go        # RequireAuth, OptionalAuth middleware
│   │   ├── user.go             # User model + UpsertUser/GetUserByID
│   │   ├── session.go          # Session CRUD + cleanup
│   │   ├── preferences.go      # Key-value user preferences
│   │   └── settings_handlers.go # Settings API endpoints
│   ├── db/
│   │   └── db.go               # SQLite init, WAL mode, schema creation
│   └── weather/
│       └── handler.go          # Weather forecast via yr.no API
├── web/                        # React + TypeScript + Vite frontend
│   ├── src/
│   │   ├── main.tsx            # Entry: React Router + AuthProvider
│   │   ├── App.tsx             # Routes + layout (Sidebar + main)
│   │   ├── auth.tsx            # AuthContext: user state, login/logout
│   │   ├── components/
│   │   │   ├── Sidebar.tsx     # Responsive nav: collapsible desktop, slide-out mobile
│   │   │   └── ProtectedRoute.tsx # Redirects to / if not authenticated
│   │   └── pages/              # One file per page
│   └── package.json
├── Makefile
└── .forge/warden-rules.yaml    # Copilot review rules learned from PR feedback
```

## Tech Stack

- **Backend**: Go 1.26 with Chi v5 router, SQLite via modernc.org/sqlite (CGO-free), WAL mode
- **Frontend**: React 19, TypeScript 5.9, Vite 7, Tailwind CSS v4, Lucide React icons
- **Auth**: Google OAuth2 only. Sessions are 64-char hex tokens in DB, 30-day expiry, HttpOnly cookies
- **Database**: SQLite with `CREATE TABLE IF NOT EXISTS` in `db.go:createSchema()` — no migration files

## Conventions

### Backend (Go)

- **Package per feature**: each feature gets `internal/<feature>/` with handlers, models, and tests
- **Handler pattern**: `func FooHandler(db *sql.DB) http.HandlerFunc` — returns a closure
- **Router registration**: add routes in `internal/api/router.go` inside the appropriate auth group
- **Auth groups**: public routes at top, `OptionalAuth` for /auth/me, `RequireAuth` for everything else
- **Schema changes**: add `CREATE TABLE IF NOT EXISTS` to `db.go:createSchema()`. Use `ALTER TABLE` for existing table changes
- **Testing**: use `setupTestDB()` with `:memory:` SQLite, test both success and failure paths
- **Time**: use Go `time.Now()` for timestamps, not SQLite `datetime('now')` — keep consistent
- **Errors**: return proper HTTP status codes with JSON `{"error": "message"}`

### Frontend (React/TypeScript)

- **Styling**: Tailwind CSS only, dark theme (bg-gray-900/950 base). No CSS modules or styled-components
- **Icons**: Lucide React, size={20} for nav, size={16-24} elsewhere
- **Auth**: use `useAuth()` hook from `auth.tsx`. Wrap protected routes in `<ProtectedRoute>`
- **State**: React hooks only (useState, useEffect). No Redux or external state management
- **API calls**: plain `fetch()` to `/api/*` endpoints with credentials: 'include'
- **Routing**: React Router v7. Add routes in `App.tsx`, nav items in `Sidebar.tsx`
- **New pages**: create in `web/src/pages/`, follow Settings.tsx as pattern for complex pages
- **i18n**: All user-facing strings MUST use `react-i18next`. Import `useTranslation` and use `t('namespace:key')` — never hardcode English text in JSX. Translation files are in `web/public/locales/{en,nb,th}/`. Add new keys to all three languages. Use `i18n.language` as the locale parameter for `Intl.DateTimeFormat`, `Intl.NumberFormat`, etc. See existing pages for patterns.

### Data Security & Encryption

- **Session tokens are hashed** (SHA-256) before storage. The cookie holds the raw token; the DB only has the hash. Use `hashToken()` from `internal/auth/session.go`.
- **Sensitive data is encrypted at rest** using AES-256-GCM via `internal/encryption/encryption.go`:
  - `encryption.EncryptField(plaintext)` → ciphertext string for DB storage
  - `encryption.DecryptField(ciphertext)` → plaintext for use in code
  - Handle decrypt errors gracefully — if decrypt fails, the value may be legacy plaintext (return as-is with a log warning)
- **Fields that MUST be encrypted on write and decrypted on read:**
  - Workout: title, notes
  - Notes: title, content
  - Lactate: stage data
  - Push subscriptions: endpoint URLs
  - VAPID: private_key
  - Analysis: prompt, response_json
  - User preferences: claude_cli_path
- **Fields that must NOT be encrypted** (needed for queries/filtering): IDs, timestamps, status fields, sport, duration, distance, heart rate, tags, labels, email
- **When adding a new feature that stores user data**: always encrypt sensitive text fields using `encryption.EncryptField()` on write and `encryption.DecryptField()` on read. If in doubt, encrypt it.
- **Encryption key** is at `~/.config/hytte/.encryption_key` (auto-generated, mode 0600). The `ENCRYPTION_KEY` env var overrides it.
- **API tokens** (Hetzner, GitHub) use the same encryption via `internal/infra/crypto.go`.

### Feature Gating

- Features are gated per-user via `user_features` table and `auth.RequireFeature(db, "feature_key")` middleware
- Admin users (`is_admin=true`) bypass all feature checks
- Available features: dashboard, weather, calendar, notes, links, training, lactate, infra, webhooks, claude_ai
- Claude AI settings/endpoints require admin (`is_admin`) — not just the `claude_ai` feature flag

### Changelog Fragments

Every PR must include a changelog fragment in `changelog.d/`:

```
category: Added
- **Short title** - Description of the change. (Hytte-xxxx)
```

Categories: `Added`, `Changed`, `Fixed`, `Removed`, `Deprecated`, `Security`. Single language (English).

### General

- **No migration files** — schema is inline in `createSchema()`, column additions use `ALTER TABLE` with existence checks
- **No env files committed** — secrets via environment variables: `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_REDIRECT_URL`, `SECURE_COOKIES`
- **User preferences** are key-value pairs in `user_preferences` table, allowed keys validated in handler
- **Test auth**: set `HYTTE_TEST_AUTH=1` env var to enable `POST /api/auth/test-login` endpoint (for QuestGiver E2E testing only, never in production)

### Wordfeud Coordinate System

**IMPORTANT — DO NOT REVERT:** The Wordfeud API returns tile positions as `[col, row]`, not `[row, col]`. When mapping API tiles to the `board[x][y]` array, we use `board[col][row]` (see `internal/wordfeud/api.go`). This swap has been verified against the official Wordfeud app and is intentional. Without it, the board renders transposed (mirrored along the diagonal). This has been incorrectly reverted by automated PRs twice — if you see `board[row][col]` for API tile placement, that is the bug, not the fix.

## API Routes

All prefixed with `/api/`.

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | /health | Public | Health check (pings DB) |
| GET | /auth/google/login | Public | Redirects to Google consent |
| GET | /auth/google/callback | Public | OAuth callback |
| POST | /auth/logout | Public | Destroys session |
| GET | /auth/me | Optional | Current user or null |
| GET | /weather/forecast | Public | Weather via yr.no |
| GET | /weather/locations | Public | Available Norwegian cities |
| GET | /settings/preferences | Required | Get user prefs |
| PUT | /settings/preferences | Required | Set a preference |
| GET | /settings/sessions | Required | List active sessions |
| POST | /settings/sessions/revoke-others | Required | Sign out other sessions |
| DELETE | /settings/account | Required | Delete account + cascade |

## CI

GitHub Actions on PR to main: `go build`, `go vet`, `go test`, `npm ci`, `npm run lint`, `npm run build`.

## Shell Safety (on Windows)

Always use non-interactive flags to avoid hanging on prompts:
```bash
cp -f source dest
rm -f file
rm -rf dir
```
