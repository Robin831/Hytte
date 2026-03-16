category: Added
- **GitHub Actions Status module** - Monitor workflow run status across configured repositories with encrypted token storage. (Hytte-7et)
- **DNS Monitoring module** - Monitor DNS resolution for configured hostnames with support for A, AAAA, CNAME, MX, TXT, and NS record types. (Hytte-7et)
- **Database Stats module** - View SQLite database size and per-table row counts scoped to the authenticated user. (Hytte-7et)

category: Security
- **SSRF protection for GitHub Actions module** - GitHub API HTTP client now uses safeDialContext to validate resolved IPs at connection time. (Hytte-7et)
- **Input validation for GitHub owner/repo fields** - Owner and repo names are validated against GitHub naming rules to prevent path injection in API URLs. (Hytte-7et)
