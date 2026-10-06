category: Changed
- **Lactate Insights loads all analyses in one request** - New `GET /api/lactate/analyses` returns the analysis of every test with at least 2 stages, keyed by test ID, replacing the per-test request burst on the Insights page. The threshold trend no longer gets stuck loading when no test is eligible. (Hytte-hhxjw)
