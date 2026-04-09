category: Fixed
- **Fix PR actions for external forge PRs** - PRs with ext- prefixed bead IDs now route PR actions through the external PR IPC pathway (`external_pr_action`) instead of silently failing via the regular forge pipeline IPC. (Hytte-q3x3)
