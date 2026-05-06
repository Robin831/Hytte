category: Changed
- **Bumped suggestion-run timeouts and rotation size** - `PerPageTimeout` increased from 90s to 240s to accommodate slow Claude calls on large pages, `OverallRunTimeout` from 10 to 30 minutes to fit the larger rotation, and `RotationDefaultN` from 10 to 20 so the ~35-page registry cycles every couple of nights. (Hytte-h22b)
