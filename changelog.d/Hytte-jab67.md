category: Fixed
- **Overlapping work sessions are now rejected** - Adding or editing a work session that overlaps another session on the same day now returns 409 with the conflicting session instead of saving double-counted hours; back-to-back sessions are still allowed. (Hytte-5fgzl)
