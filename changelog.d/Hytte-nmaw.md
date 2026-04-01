category: Fixed
- **Fix case-sensitive anvil name lookup in Forge Dashboard** - Bead IDs have capitalized prefixes (e.g. 'Hytte-abc1') but config.yaml uses lowercase anvil keys. The lookup is now case-insensitive so bead detail, label, and comment commands work correctly. (Hytte-nmaw)
