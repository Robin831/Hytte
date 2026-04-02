category: Fixed
- **Forge release: find forge binary via PATH** - `forgeBin()` now uses `exec.LookPath("forge")` as a fallback before `~/.forge/forge`, so the release handler works when the binary is installed in non-standard locations such as `/home/robin/bin/forge`. (Hytte-53og)
- **Wordfeud: fall back to per-player rack when game-level rack is absent** - `toGameState()` now checks the player's own rack entry when the top-level rack field is empty, fixing missing rack tiles for API responses that omit `is_local` and store the rack only in the player object. (Hytte-53og)
