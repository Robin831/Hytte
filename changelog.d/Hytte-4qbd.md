category: Changed
- **Suggestions parser no longer requires distinct sizes per page** - Per-page suggestion responses still need three distinct types, but two or three suggestions may now share the same size. The prompt is updated to make sizes a soft preference, reducing wasted retries on otherwise-valid Claude responses. (Hytte-4qbd)
