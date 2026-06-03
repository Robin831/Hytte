category: Changed
- **Instant weather forecast on revisit** - The Weather page now renders the last successful forecast for a location immediately from a local cache (stale-while-revalidate), then refreshes it in the background, so revisits no longer flash skeleton placeholders. The cache keeps up to five recently viewed locations. (Hytte-2e8d)
