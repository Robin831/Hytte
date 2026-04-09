category: Fixed
- **Fix PR actions for external forge PRs** - PRs with ext- prefixed bead IDs now route approve and merge through the GitHub CLI, and other actions through the external PR IPC pathway, instead of silently failing via the regular forge pipeline IPC. (Hytte-q3x3)
