category: Added
- **Stride evaluation API endpoints** - Added `GET /api/stride/evaluations` to list stored AI workout evaluations (filterable by `plan_id`), and `POST /api/stride/evaluate` to manually trigger evaluation of the past 24 hours of unevaluated workouts for the authenticated user. Both endpoints require the `stride` feature flag. (Hytte-eluy)
