category: Added
- **Suggestion CRUD endpoints** - Added admin-only `GET /api/suggestions` (partitioned by status with decrypted bodies), `POST /api/suggestions` (user-authored pending suggestions, validated against the type/size/page registry), `POST /api/suggestions/{id}/reject` (idempotent), and `GET /api/suggestions/pages` (registry plus the synthetic `__new_page__` entry). (Hytte-tdui)
