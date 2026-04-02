category: Fixed
- **Fix Forge costs/beads endpoint 500 error** - Updated the bead_costs query to match the actual state.db schema: use `updated_at` column instead of `date`, include `cache_read`/`cache_write` columns, and use RFC3339 timestamps for date filtering. (Hytte-sscm)
