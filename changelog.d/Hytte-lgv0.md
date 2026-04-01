category: Added
- **Backend: GET /api/infra/versions endpoint** - New endpoint that shells out to common tools (claude, forge, bd, go, node, npm, gh, git, dolt) and reports their versions, plus the current HEAD commit of the Forge repo. Results are cached in-memory for 5 minutes; individual command failures return an error string rather than failing the whole request. (Hytte-lgv0)
