category: Added
- **Tasks backend foundation** - Added schema (`tasks`, `task_tags`, `task_tag_assignments`, `task_notes`), the `tasks` feature flag (default off), and `/api/tasks` REST endpoints (list/create/patch/delete plus per-task notes) gated by `RequireFeature("tasks")`. Title, body, tag labels and note content are encrypted at rest. (Hytte-u49p)
