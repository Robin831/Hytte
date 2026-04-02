category: Fixed
- **Forge release: find forge binary via PATH** - `forgeBin()` now uses `exec.LookPath("forge")` as a fallback before `~/.forge/forge`, so the release handler works when the binary is installed in non-standard locations such as `/home/robin/bin/forge`. (Hytte-53og)
