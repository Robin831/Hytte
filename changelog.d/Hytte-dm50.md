category: Added
- **Long and tempo run types in workout context** - Workout context modal now offers four run types (slow, long, tempo, interval), with values persisted and round-tripped across saves. (Hytte-dm50)
- **Minutes and seconds inputs in speed plan editor** - Per-row interval and shared pause durations are entered as minutes plus seconds in the speed plan editor; existing values stored as `duration_sec` continue to load and edit without data loss. (Hytte-dm50)

category: Changed
- **Auto-run AI summary on workout context save** - Saving the workout context modal now flips `analysis_status` to pending and triggers the same background Claude analysis used at FIT upload, so the summary completes via polling without a manual Analyze click. Skips when an analysis is already running or completed. (Hytte-dm50)
