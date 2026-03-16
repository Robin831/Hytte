category: Fixed
- **Fix Hetzner API token saving failing with 'failed to save token'** - Auto-generate a persistent encryption key when ENCRYPTION_KEY env var is not set, so token storage works out of the box. Key file is stored in the user config directory for reliability. Corrupt key files (wrong length, invalid hex, trailing whitespace) are detected and regenerated automatically. (Hytte-7o2)
