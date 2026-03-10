# Copilot Instructions

## Project Overview

Hytte is a personal playground/dashboard app with a Go backend (Chi router, SQLite) and React frontend (TypeScript, Vite, Tailwind CSS v4). Authentication is Google OAuth2 with session cookies.

## Architecture

- **Backend**: Go packages under `internal/` — one package per feature (auth, weather, etc.)
- **Frontend**: React 19 + TypeScript 5.9 + Vite 7 + Tailwind CSS v4 + Lucide React icons
- **Database**: SQLite with WAL mode, CGO-free via modernc.org/sqlite. Schema defined inline in `internal/db/db.go:createSchema()` using `CREATE TABLE IF NOT EXISTS`
- **Auth**: Google OAuth2 → session token in HttpOnly cookie → `RequireAuth`/`OptionalAuth` middleware

## Code Conventions

### Go
- Handler pattern: `func XxxHandler(db *sql.DB) http.HandlerFunc`
- Routes registered in `internal/api/router.go` inside auth groups (public, optional, required)
- Tests use `:memory:` SQLite with `setupTestDB()` helper
- Use `time.Now()` for timestamps, not `datetime('now')` in SQL
- Return JSON errors: `{"error": "message"}` with appropriate HTTP status

### TypeScript/React
- Tailwind CSS only for styling — dark theme (bg-gray-900/950)
- Use `useAuth()` hook for authentication state
- Wrap protected routes in `<ProtectedRoute>` component
- Use `fetch()` with `credentials: 'include'` for API calls
- Locale-sensitive formatting: pass `undefined` as locale to respect browser settings
- Icons: Lucide React, typically size={20}

### Testing
- New DB operations need unit tests covering insert, update, retrieval
- New HTTP handlers need httptest-based tests for success and error paths
- Follow existing test patterns in the same package

## Review Checklist

See `.forge/warden-rules.yaml` for detailed review rules learned from previous PR feedback. Key items:
- Async operations in useEffect must have error handling
- Loading state must reset in `finally` blocks
- Protected route nav links only shown when authenticated
- No unused imports
- Comments must match actual code behavior
- String bounds checked before slicing
- Lock-protected fields copied before unlock
- Sorted output from map iteration for stable API responses
