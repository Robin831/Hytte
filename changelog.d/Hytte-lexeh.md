category: Fixed
- **Weather recents no longer diverge from the server when a selection races the preferences load** - Picking a location before saved preferences arrived made the dropdown show the server's recent locations while pushing the local list (containing the new pick) back to the server. The local list now wins for both the displayed recents and the saved preference. (Hytte-lexeh)
