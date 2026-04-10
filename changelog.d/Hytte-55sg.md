category: Changed
- **Weekly plan generation now consumes notes** - Weekly plan generation fetches only unconsumed notes (instead of the 20 most recent) and marks them as consumed atomically within the same DB transaction as the plan upsert. Notes consumed by the nightly evaluation are excluded, and a failed plan insert rolls back note consumption. (Hytte-55sg)
