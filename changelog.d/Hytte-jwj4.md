category: Added
- **forge IPC client** - Added `internal/forge/ipc.go` with a `Client` struct that connects to the forge daemon via Unix socket (`~/.forge/forge.sock` or `FORGE_IPC_SOCKET` env var), providing `SendCommand` for fire-and-response IPC and `Health` for daemon liveness checks. (Hytte-jwj4)
