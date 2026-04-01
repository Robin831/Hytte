category: Fixed
- **Atomic dist swap during deploys** - Build frontend to a temp directory and atomically swap it in, preventing 404 errors during deploys when dist/index.html is temporarily missing. (Hytte-1zop)
