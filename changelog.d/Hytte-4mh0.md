category: Changed
- **Extract minute-tick clock into a reusable hook** - Moved the minute-aligned clock logic out of TodayView into a new `useCurrentTime` hook that ticks on wall-clock minute boundaries and pauses while the tab is hidden. No change to the Today page behavior. (Hytte-4mh0)
