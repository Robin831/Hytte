category: Fixed
- **Lactate tests loading flicker** - Gate the skeleton render on auth readiness so a slow auth bootstrap no longer flashes skeleton → empty → content. The request-loading flag now starts false and is set only while a tests request is genuinely in flight. (Hytte-jbiy)
