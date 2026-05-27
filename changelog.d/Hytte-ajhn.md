category: Fixed
- **Scope sql.Rows lifetimes in dashboard recent activity** - Refactor `recentActivity` to query each source (workouts, lactate, notes, short links) in its own helper with a locally scoped `defer rows.Close()`, preventing a `sql.Rows` handle from staying open if a later source query fails. (Hytte-ajhn)
