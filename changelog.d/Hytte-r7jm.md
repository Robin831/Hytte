category: Added
- **Netatmo historical readings storage** - Added `netatmo_readings` SQLite table and `StoreReadings`/`QueryHistory` functions in the netatmo package. Readings are stored per-user with a 7-day retention policy enforced on every write. (Hytte-r7jm)
