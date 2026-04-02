category: Added
- **Full action buttons for external PRs** - External PRs in the forge dashboard now have Fix Comments, Fix CI, Fix Conflicts, Bellows, and Reset Counters action buttons, matching the functionality available for forge-managed PRs. (Hytte-v57n)
- **Wordfeud client corrections** - The Wordfeud API client now uses the correct HTTP method (POST) for the games list and game detail endpoints, which the upstream server requires to avoid redirect responses. Documents the rationale inline and locks in the method with test assertions. (Hytte-v57n)
