category: Fixed
- **Stricter race target-time parsing** - Race target times must now be entered as `H:MM:SS` (e.g. `3:30:00`). Two-part inputs like `25:00` are rejected with a clear validation error instead of being silently inflated to 25 hours. (Hytte-aabh)
