category: Security
- **Route authentication enforcement** - All routes except the landing page (/) now require authentication. Unauthenticated users are redirected to the landing page. Backend API routes return 401 for unauthenticated requests (except /api/health and /api/auth/*). (Hytte-8me)
