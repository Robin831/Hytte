category: Fixed
- **Bead detail modal now works for all anvils** - The bead detail handler was using the wrong working directory for `bd show` commands, causing failures for beads from non-Hytte anvils. Now uses `anvilDirForBead()` to resolve the correct repo path from forge config. (Hytte-8608)
