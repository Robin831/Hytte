category: Added
- **Badge API endpoints** - Added `GET /api/stars/badges` to list earned badges for the authenticated user, and `GET /api/stars/badges/available` to list all badge definitions with earned/unearned status (unearned secret badges are filtered server-side). Both routes are gated by the `kids_stars` feature. (Hytte-0sgn)
