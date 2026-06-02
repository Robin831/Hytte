category: Changed
- **Extract minute-tick clock into a reusable hook** - Moved the minute-aligned clock logic out of TodayView into a new `useCurrentTime` hook that ticks on wall-clock minute boundaries. The hook pauses while the tab is hidden and snaps to the current time on resume. (Hytte-4mh0)
