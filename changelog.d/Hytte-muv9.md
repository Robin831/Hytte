category: Added
- **Backend tests for reward & claim logic** - Added unit tests for ClaimReward happy path, insufficient balance with no side effects verification, max claims enforcement, concurrent claim race condition prevention, ResolveClaim approve/deny paths, authorization checks, and encryption of all sensitive fields (title, description, parent_note, resolution note). (Hytte-muv9)

category: Changed
- **ClaimReward retry on database busy** - ClaimReward now retries up to 10 times on SQLITE_BUSY/SQLITE_LOCKED errors instead of surfacing a raw lock error to callers. Exhausted retries return a typed `ErrDatabaseBusy` sentinel (suitable for HTTP 503). (Hytte-muv9)
