category: Added
- **Workout training metrics fields** - Added `training_load`, `hr_drift_pct`, and `pace_cv_pct` nullable columns to the workouts table and corresponding `*float64` fields to the Workout struct, enabling downstream computation of training load and quality metrics. (Hytte-53c7)
