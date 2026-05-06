category: Added
- **Admin endpoints for suggestion page rotation settings** - `GET /api/suggestions/pages` now includes the per-page `rotation_enabled` override (null when no override exists, defaulting to on), and a new `PATCH /api/suggestions/pages/{slug}` upserts the override. Both are admin-only. (Hytte-1waa)
