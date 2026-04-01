category: Fixed
- **Fix add/remove label silently failing on forge dashboard** - Label operations now invoke `bd label add/remove` directly instead of going through IPC, avoiding the 5-second read timeout that caused the action to silently fail. (Hytte-cb1e)
