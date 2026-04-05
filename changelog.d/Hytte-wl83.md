category: Added
- **Stride GeneratePlan core logic** - Adds `internal/stride/generate.go` with `GeneratePlan(ctx, db, userID)` that queries race calendar, notes, training load/ACR, and recent volume history to build a Marius Bakken threshold-dominant training plan via Claude AI. Prompt and response are encrypted at rest in `stride_plans`. (Hytte-wl83)
