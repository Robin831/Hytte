category: Fixed
- **Fix missing latest versions for bd, Claude, and Git** - Fixed bd fetcher pointing to wrong GitHub repo (Robin831/beads instead of steveyegge/beads), replaced git fetcher with tags API to avoid empty releases endpoint, and switched Claude fetcher from unreliable CLI command to npm registry lookup. (Hytte-lf73)
