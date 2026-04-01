category: Added
- **Release API endpoint** - New POST /api/forge/release endpoint that executes the release pipeline server-side: pulls latest main, assembles changelog, removes fragments, commits, tags, and pushes. Admin-only with semver validation and step-by-step result reporting. (Hytte-9a4g)
