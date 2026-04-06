category: Fixed
- **Fix dropdown clipping in NeedsAttentionCard** - The action menu dropdown was clipped by `overflow-hidden` on the card container. The dropdown now renders via a React portal into `document.body` with `position: fixed`, positioned relative to the trigger button, so it always appears above the overflow boundary. (Hytte-oivl)
