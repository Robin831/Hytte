category: Added
- **Workout name extraction from FIT file metadata** - When importing a .fit file, the workout title is now extracted from the FIT session's SportProfileName or FileId ProductName fields if available, instead of always auto-generating a "Sport YYYY-MM-DD HH:MM" title. Falls back to the auto-generated title when no name is found in the metadata. (Hytte-74b)
