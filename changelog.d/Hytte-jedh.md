category: Added
- **Chore photo upload and serving** - Children can now attach a photo when marking a chore as done (multipart POST to `/api/allowance/my/complete/{id}`). Photos are stored in `data/chore-photos/` and served via `GET /api/allowance/photos/{completion_id}`. Photos older than 7 days are automatically cleaned up. (Hytte-jedh)
