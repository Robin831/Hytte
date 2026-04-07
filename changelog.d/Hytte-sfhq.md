category: Fixed
- **Chat page second message no longer times out** - The Claude CLI runner had a hardcoded 60-second timeout that overrode the caller's longer timeout, causing multi-turn conversations to fail with 'signal: killed'. Now respects the caller's deadline. (Hytte-sfhq)
