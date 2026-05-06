category: Added
- **Suggestions: create bead from planned suggestions** - Wired the Planned-card "Create bead" button up to a new `POST /api/suggestions/{id}/bead` endpoint that shells out to `bd create` with the `forgeReady` label and links the resulting Hytte- bead back to the suggestion. The Planned tab now shows a "Created beads" section for suggestions that have already shipped. (Hytte-id25)
