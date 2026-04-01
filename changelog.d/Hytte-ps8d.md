category: Fixed
- **Fix Forge Dashboard IPC timeout** - Replace IPC client calls with direct state.db reads, exec.Command for CLI operations, and fire-and-forget socket writes for daemon signals. Eliminates the 5-second read timeout that caused dashboard data loading to hang and mutation commands to fail. (Hytte-ps8d)
