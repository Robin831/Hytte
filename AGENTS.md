# AGENTS.md

Instructions for AI agents (Claude Code, Copilot, etc.) working on this codebase.

## Code Review Rules

When reviewing PRs, check against the warden rules in `.forge/warden-rules.yaml`. Key areas:

### UI
- Persisted UI state (localStorage) must be accessible at all breakpoints — don't hide controls on mobile for state that affects all views
- Auth actions (sign out, profile) must remain accessible when refactoring nav/layout
- Nav links to protected routes should only render when authenticated
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

### Security
- Public route lists in docs/changelog must match actual code
- Comments describing auth/redirect behavior must match implementation

### Style
- No unused imports
- Comments must accurately describe the code behavior, not aspirational behavior
- Locale-sensitive formatting: use `undefined` locale to respect browser settings

### Concurrency (Go)
- Never read lock-protected fields after releasing the lock — copy to locals first
- Every read of a concurrently-mutated field needs at least an RLock

### Testing
- New DB persistence logic needs unit tests (insert, update, retrieval, round-trip)
- New HTTP handlers need httptest-based tests for success and failure paths

### Other
- Time comparisons: use Go `time.Now()` consistently, not mixed with SQLite `datetime('now')`
- Timestamps in API responses: parse to `time.Time`, serialize as RFC3339
- Constant lists (cities, codes): define once, import everywhere — no duplication
- String matching chains: specific matches before general ones (e.g. 'heavyrain' before 'rain')
- Changelog entries must match actual implementation

## Adding a New Feature

1. **Backend**: create `internal/<feature>/` package with handlers + tests
2. **Schema**: add tables to `internal/db/db.go:createSchema()`
3. **Routes**: register in `internal/api/router.go` under the correct auth group
4. **Frontend**: create page in `web/src/pages/`, add route in `App.tsx`
5. **Navigation**: add nav item to `Sidebar.tsx` with Lucide icon
6. **Tests**: Go unit tests with `:memory:` SQLite, cover happy + error paths
