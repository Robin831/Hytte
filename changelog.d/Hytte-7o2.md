category: Fixed
- **Fix Hetzner API token saving failing with 'failed to save token'** - Auto-generate a persistent encryption key when ENCRYPTION_KEY env var is not set, so token storage works out of the box. The env var still takes precedence for production deployments. (Hytte-7o2)
