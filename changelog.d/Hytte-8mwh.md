category: Added
- **Latest version fetching service** - New API endpoint `/api/infra/latest-versions` that fetches the latest available upstream versions for all monitored tools (Forge, bd, gh, Dolt, Go, Node, npm, Git, Claude) with 1-hour caching, singleflight deduplication, and stale-cache fallback on failure. (Hytte-8mwh)
