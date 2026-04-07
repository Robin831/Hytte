category: Changed
- **Dashboard action handlers use Forge IPC socket** - RetryBead, DismissBead, ApproveBead, and ForceSmith handlers now send IPC commands (`retry_bead`, `dismiss_bead`, `approve_as_is`, `force_smith`) directly to the forge daemon socket instead of spawning a `forge queue` subprocess. (Hytte-gfkd)
