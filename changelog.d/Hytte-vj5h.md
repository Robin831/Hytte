category: Added
- **Workout context API** - Added per-workout context storage with `GET` and `PUT /api/training/workouts/{id}/context` endpoints for capturing surface, run type, HR source, free-text feel notes, and a structured speed plan. Sensitive fields (feel notes, speed plan JSON) are encrypted at rest. (Hytte-vj5h)
