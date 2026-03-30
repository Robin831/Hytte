category: Fixed
- **Webhooks: use summary field for richer Forge push notifications** - `formatForgeNotification` now prefers the `summary` field (which contains the PR title) over `message` when building the notification body, so mobile push notifications show what the PR is actually about. (Hytte-lc71)
