category: Fixed
- **Lactate tests loading flicker** - Gate the skeleton render on auth readiness so a slow auth bootstrap no longer flashes skeleton → empty → content. The loading flag now stays set through auth resolution and is only cleared once auth resolves with no user or a tests request completes, so the first paint is stable on every path. (Hytte-jbiy)
