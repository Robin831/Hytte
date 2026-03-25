category: Added
- **Star engine awards XP and sends level-up push notifications** - After recording star awards, the engine now calls `AddXP` with the total positive stars earned. If the child levels up, push notifications are sent to both the child ("LEVEL UP! You're now a {Title} (Level {N})!") and their parent ("{Nickname} leveled up!"). (Hytte-620d)
- **Balance API includes full level progress** - `GET /api/stars/balance` now returns `emoji`, `xp_for_next_level`, and `progress_percent` fields from the level system in addition to the existing level, xp, and title fields. (Hytte-620d)
