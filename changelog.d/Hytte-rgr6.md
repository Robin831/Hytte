category: Fixed
- **Fix misleading 'Update available' for apt-installed tools** - Git, GitHub CLI, and Node.js version checks now query `apt-cache policy` instead of upstream GitHub/website releases, so 'Update available' only appears when apt actually has a newer version to install. (Hytte-rgr6)
