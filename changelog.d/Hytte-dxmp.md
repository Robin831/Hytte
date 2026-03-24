category: Added
- **Compute metrics on FIT upload** - Training metrics (HR drift, pace CV, training load) are now computed and stored automatically when a FIT file is uploaded. The user's `max_hr` preference overrides the device-reported max HR for training load calculation. (Hytte-dxmp)
- **Metrics backfill endpoint** - Added `POST /api/training/metrics/backfill` (requires `training` feature) that recomputes all three metrics for any workouts missing a `training_load` value and returns the count of updated rows. (Hytte-dxmp)
