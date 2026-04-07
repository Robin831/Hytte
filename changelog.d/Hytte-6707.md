category: Changed
- **Chat uses Claude CLI session resumption** - Multi-turn conversations now use Claude CLI's `--resume` flag instead of re-sending full message history. This provides proper conversation context, reduces token usage, and improves response speed on longer conversations. (Hytte-6707)
