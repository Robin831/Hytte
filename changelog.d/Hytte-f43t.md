category: Added
- **internal/forge: SQLite state.db read layer** - Adds `internal/forge/db.go` with a read-only `DB` wrapper for the forge state database (`~/.forge/state.db`). Provides query helpers for workers, PRs, events, retries, costs, and queue cache. Configurable via `FORGE_STATE_DB` env var. (Hytte-f43t)

category: Fixed
- **Kiosk sun times only shown with explicit location** - Sun times are now only computed and returned when a location is explicitly configured in the kiosk token, rather than falling back to Bergen. Weather forecast continues to default to Bergen when no location is set. (Hytte-f43t)
