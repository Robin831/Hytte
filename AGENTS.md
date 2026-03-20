# AGENTS.md

Instructions for AI agents (Claude Code, Copilot, etc.) working on this codebase.

## Code Review Rules

When reviewing PRs, check against the warden rules in `.forge/warden-rules.yaml`. Key areas:

### UI
- Persisted UI state (localStorage) must be accessible at all breakpoints — don't hide controls on mobile for state that affects all views
- Auth actions (sign out, profile) must remain accessible when refactoring nav/layout
- Nav links to protected routes should only render when authenticated AND the user has the required feature enabled
- Don't return null/empty for the entire app shell while auth loads — only gate private sections
- Hard-coded `calc(100vh - Xpx)` must stay in sync when layout elements change
- Form controls need accessible labels (`<label>`, `aria-label`, or `aria-labelledby`)
- Map iteration for API responses must be sorted for stable order
- Date grouping for display must use local time, not UTC

### Error Handling
- All async operations in `useEffect` need `try/catch` or `.catch()`
- Loading/saving state flags must reset in `finally`, not just the success path
- String/slice indexing needs bounds checks or `min(N, len(s))`
- `io.LimitReader` silently truncates — use `io.LimitedReader` with N+1 and check overflow
- Stale cache fallback should extend cache TTL to avoid hammering a failing upstream

### Security & Encryption
- **All sensitive user data must be encrypted at rest** using `internal/encryption/encryption.go`
- Use `encryption.EncryptField()` on write, `encryption.DecryptField()` on read
- Encrypted fields: workout title/notes, note title/content, lactate stage data, push endpoints, VAPID private key, analysis prompt/response, claude_cli_path
- Do NOT encrypt fields needed for SQL queries: IDs, timestamps, sport, duration, distance, HR, tags, email
- Session tokens must be hashed (SHA-256) — never store raw tokens in the DB
- Public route lists in docs/changelog must match actual code
- Comments describing auth/redirect behavior must match implementation
- When adding new features: encrypt sensitive text fields by default

### Feature Gating
- All new feature routes must be wrapped in `auth.RequireFeature(db, "feature_key")` middleware
- Admin-only features (Claude AI settings) use `auth.RequireAdmin()` middleware
- Frontend: check `user.features[key]` before rendering feature-specific UI/nav items
- Admin users bypass all feature checks — don't add redundant admin checks inside feature-gated routes

### Style
- No unused imports
- Comments must accurately describe the code behavior, not aspirational behavior
- Locale-sensitive formatting: use `undefined` locale to respect browser settings
- No emojis in code unless the user explicitly requests them
- Use shared tag helpers from `web/src/tags.ts` (isAutoTag, isAITag, displayTag) — don't hardcode `auto:` or `ai:` prefixes

### Concurrency (Go)
- Never read lock-protected fields after releasing the lock — copy to locals first
- Every read of a concurrently-mutated field needs at least an RLock

### Testing
- New DB persistence logic needs unit tests (insert, update, retrieval, round-trip)
- New HTTP handlers need httptest-based tests for success and failure paths
- Use `setupTestDB()` with `:memory:` SQLite in tests

### Changelog
- Every PR must include a `changelog.d/<bead-id>.md` fragment
- Format: `category: Added|Changed|Fixed|Removed|Deprecated|Security` on line 1, then markdown bullets
- Include bead ID reference: `(Hytte-xxxx)` at end of each bullet
- Do NOT use commit-message style (`fix: description`) — use the fragment format

### Other
- Time comparisons: use Go `time.Now()` consistently, not mixed with SQLite `datetime('now')`
- Timestamps in API responses: parse to `time.Time`, serialize as RFC3339
- Constant lists (cities, codes): define once, import everywhere — no duplication
- String matching chains: specific matches before general ones (e.g. 'heavyrain' before 'rain')
- Changelog entries must match actual implementation
- `bd` CLI commands: use `executil.HideWindow` wrapper on Windows, set `cmd.Dir` to anvil path

## Adding a New Feature

1. **Backend**: create `internal/<feature>/` package with handlers + tests
2. **Schema**: add tables to `internal/db/db.go:createSchema()`. Use `ALTER TABLE` with existence checks for adding columns to existing tables
3. **Encrypt sensitive data**: use `encryption.EncryptField()` / `encryption.DecryptField()` for any user-generated text content
4. **Routes**: register in `internal/api/router.go` under the correct auth + feature gate group
5. **Feature gate**: add the feature key to `auth.FeatureDefaults` map and wrap routes with `auth.RequireFeature(db, "key")`
6. **Frontend**: create page in `web/src/pages/`, add route in `App.tsx` with feature check
7. **Navigation**: add nav item to `Sidebar.tsx` with Lucide icon, gated by `hasFeature("key")`
8. **Tests**: Go unit tests with `:memory:` SQLite, cover happy + error paths
9. **Changelog**: create `changelog.d/<bead-id>.md` with proper format

## Claude AI Integration

- Claude CLI is installed on the server at `/home/robin/.local/bin/claude`
- Configuration stored in user_preferences: `claude_enabled`, `claude_cli_path`, `claude_model`
- Use `training.LoadClaudeConfig()` to read config, `training.RunPrompt()` to call Claude
- Claude features require both `training` + `claude_ai` feature flags
- Claude settings UI is admin-only (`is_admin=true`)
- Analysis results are cached in `workout_analyses` table — check cache before calling Claude
- Prompt and response_json must be encrypted when stored

## Test Auth (E2E Testing)

- Set `HYTTE_TEST_AUTH=1` env var to enable `POST /api/auth/test-login`
- Creates a test admin user with a session cookie — for QuestGiver/Adventurer automated testing only
- NEVER available in production (guarded by env var)
- Quest files live in `.forge/quests/*.yaml`
