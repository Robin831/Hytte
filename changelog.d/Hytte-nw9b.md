category: Fixed
- **Release panel refresh now shows latest fragments** - The suggest endpoint now runs `git fetch origin main` + `git reset --hard` before reading changelog fragments and tags, so refreshing the release panel reflects PRs merged since the last sync. (Hytte-nw9b)
