category: Added
- **Auto-analysis on upload** - Extracts `RunInsightsAnalysis` as a standalone function and hooks it into the upload flow: when the `ai_auto_analyze` preference is enabled, insights are generated automatically after each upload alongside Claude analysis. (Hytte-7luq)
- **VO2max estimation on upload** - Estimates and persists VO2max from each uploaded workout using resting HR preference, no manual action required. (Hytte-7luq)
- **Workout type distribution in AI prompts** - Adds `GetWorkoutTypeDistribution` to query AI-tagged workout types over recent weeks, included in historical context for richer coaching insights. (Hytte-7luq)
- **Race predictions in AI prompts** - Includes Riegel-based race time predictions in the historical context block sent to Claude, giving the AI awareness of the user's current fitness level. (Hytte-7luq)
- **`ai_auto_analyze` preference** - New user preference that controls automatic insights generation on workout upload. (Hytte-7luq)
