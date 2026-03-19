category: Fixed
- **Reimporting workouts after delete no longer fails with UNIQUE constraint** - SQLite `PRAGMA foreign_keys=ON` is a connection-level setting; the database connection pool could open new connections that lacked the pragma, causing `ON DELETE CASCADE` to silently not fire. Fixed by capping the pool to one open connection so the pragma is always in effect. (Hytte-c93)
