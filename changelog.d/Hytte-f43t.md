category: Added
- **internal/forge: SQLite state.db read layer** - Adds `internal/forge/db.go` with a read-only `DB` wrapper for the forge state database (`~/.forge/state.db`). Provides query helpers for workers, PRs, events, retries, costs, and queue cache. Configurable via `FORGE_STATE_DB` env var. (Hytte-f43t)
