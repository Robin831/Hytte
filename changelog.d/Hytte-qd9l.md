category: Fixed
- **Workout HR zone distribution now uses stored zone boundaries** - Zone calculation in workout detail view now reads per-user HR zone boundaries from `user_preferences` (custom zones or max HR) via the shared `hrzones` package, replacing the previous hardcoded threshold-based percentages. (Hytte-qd9l)
