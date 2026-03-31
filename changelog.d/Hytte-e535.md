category: Fixed
- **Forge Dashboard no longer shows 'Daemon offline' when a smith is active** - The IPC health check now uses a dial-only connection test instead of a full ping/pong exchange. This avoids the 5s read timeout that fired when the daemon was busy processing workers and slow to respond, causing the dashboard to incorrectly report the daemon as offline. (Hytte-e535)
