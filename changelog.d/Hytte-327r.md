category: Fixed
- **Forge Dashboard PR actions use correct JSON IPC format** - All PR action buttons now send JSON pr_action commands to the forge daemon (burnish, quench, rebase, merge, approve_as_is, bellows, close) instead of legacy text-based commands. Actions that operate on a branch include the branch name from the prs table. Added Close PR button. (Hytte-327r)
