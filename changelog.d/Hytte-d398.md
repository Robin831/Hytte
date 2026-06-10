category: Added
- **Infra dashboard auto-refresh** - The Infra status page now refreshes automatically about every 60 seconds while the tab is visible. Polling pauses when the tab is hidden and resumes with an immediate refresh on return, and consecutive poll failures back off exponentially (up to 8 minutes) until the next successful poll. (Hytte-d398)
