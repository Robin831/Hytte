category: Fixed
- **Auto-cleanup orphaned workout child rows on startup** - Databases with orphaned rows in workout_laps, workout_samples, or workout_tags (left behind by deletes before the cascade fix) are now automatically cleaned up when the app starts, unblocking .fit file reimports. (Hytte-ccf)
