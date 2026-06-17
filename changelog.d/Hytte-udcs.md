category: Changed
- **Transit countdowns tick every second** - Relative departure labels on the Transit page now recompute every second against the current time, so a "5 min" label drops to "4 min" right at the minute boundary instead of staying frozen until the next 30s data refresh. No extra network requests are made. (Hytte-udcs)
