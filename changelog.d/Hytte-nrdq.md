category: Added
- **Enriched workout metrics block in AI prompts** - Added `BuildEnrichedWorkoutBlock` which formats HR drift, pace CV, training load, and ACR (Acute:Chronic Workload Ratio) with interpretation hints into a structured text block that is now injected into Claude workout insight prompts. (Hytte-nrdq)
- **ACR trend endpoint** - New `GET /api/training/acr-trend` endpoint returns weekly Acute:Chronic Workload Ratio data points over the past 26 weeks (configurable via `?weeks=N`, max 104) for use in training load monitoring dashboards. (Hytte-nrdq)
