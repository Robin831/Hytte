category: Changed
- **Debounced Notes search** - Typing in the Notes search box now waits 250ms after you stop before fetching, so a multi-character query fires a single `/api/notes?search=...` request instead of one per keystroke. The input stays instantly responsive and in-flight requests are still cancelled, keeping the list consistent on slow connections. (Hytte-3g8r)
