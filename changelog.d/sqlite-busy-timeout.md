category: Fixed
- **Occasional "database is locked" errors** - SQLite connections now wait up to 5 seconds for another write to finish (`busy_timeout`) instead of failing immediately with SQLITE_BUSY when two requests write at the same time.
