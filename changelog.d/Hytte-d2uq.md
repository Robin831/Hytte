category: Added
- **Challenge expiry reminder notifications** - Added `CheckChallengeExpiry` scheduler function that sends push reminders to children when active challenges expire in 2 days or 1 day. Each (challenge, milestone) pair is delivered at most once via deduplication in `daemon_notification_sent`. Fires during the 10:xx hour in the child's configured timezone. (Hytte-d2uq)
