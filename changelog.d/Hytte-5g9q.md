category: Fixed
- **Fix latest version fetchers for bd, git, and Claude CLI** - Use repository ID URL for bd to avoid GitHub redirect failures, query tags API for git/git (which has no GitHub Releases), and fetch Claude CLI version from npm registry instead of non-existent `--check` flag. (Hytte-5g9q)
