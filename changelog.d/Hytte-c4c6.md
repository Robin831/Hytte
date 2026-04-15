category: Fixed
- **Mezzanine PR panel actions now work for ext- bead PRs** - Assign Bellows, Fix Comments, and other action buttons on external bead PRs in the Mezzanine panel no longer silently no-op. All PR actions now route through the daemon's pr_action handler regardless of bead ID prefix. (Hytte-c4c6)
