category: Changed
- **Deep-linkable Wordfeud tabs** - The active Wordfeud tab (finder/board) is now driven by the `?tab=` URL query param instead of localStorage, so tabs are shareable and restored on refresh across devices. The legacy `wordfeud-tab` localStorage value is migrated once and then cleared. (Hytte-z9fe)
