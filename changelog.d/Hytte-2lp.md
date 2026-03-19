category: Security
- **Admin-only gating for Claude AI preferences** - Claude-related preferences (claude_enabled, claude_cli_path, claude_model) are now filtered from the GET response and rejected on PUT for non-admin users. The first registered user (ID 1) is automatically set as admin. (Hytte-2lp)
