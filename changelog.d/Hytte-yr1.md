category: Added
- **Notification filters** - Per-source (GitHub, generic) and per-event-type (push, pull request, release) toggles in notification settings. Filtered events are silently skipped in the push dispatch pipeline. Filter preferences are pre-fetched to avoid per-dispatch DB queries. (Hytte-yr1)
