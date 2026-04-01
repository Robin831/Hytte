category: Fixed
- **Node.js version check uses apt repository instead of nodejs.org** - The version checker now queries `apt-cache policy nodejs` for the latest available version within the configured apt source, instead of scraping nodejs.org for the absolute latest LTS. This eliminates misleading "Update available" warnings when the installed version matches the pinned major line. (Hytte-jl0o)
