category: Added
- **Frontend feature gating** - Routes, navigation items, and dashboard widgets are now gated based on user feature permissions from `/api/auth/me`. Users only see pages and nav links for features they have access to. Admin-only routes are gated by `is_admin`. Loading states show a spinner instead of blank content. (Hytte-kkz)
