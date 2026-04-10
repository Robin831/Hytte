category: Added
- **Homework student-facing API routes** - Added student-facing endpoints (/api/homework/profile, /api/homework/conversations, etc.) gated by RequireChild middleware so children can access their own homework data directly. Parent/admin routes now require RequireParentOrAdmin middleware. (Hytte-vejn)
