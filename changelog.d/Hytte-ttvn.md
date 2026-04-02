category: Fixed
- **Wordfeud login redirect handling** - Fix login failure when the Wordfeud API returns a relative redirect URL by resolving it against the current request URL. (Hytte-ttvn)
- **Fix Comments button always visible** - The Fix Comments button on the forge dashboard is now always shown for all PRs, so users can trigger comment fixes even when the unresolved threads flag is stale. (Hytte-ttvn)
- **Reset fix counters from dashboard** - Added a Reset Counters button on PRs that have hit fix attempt limits, allowing bellows to retry fixing comments and CI. (Hytte-ttvn)
