category: Changed
- **Refactored the Work Hours page into modular components** - Split the single large `WorkHoursPage` module into per-view components (day, week, month, settings), shared types/date helpers, and a typed `useWorkHoursApi` hook that centralizes all work-hours API calls. No change to behavior or layout. (Hytte-ieus)
