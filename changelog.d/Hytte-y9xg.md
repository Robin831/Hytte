category: Fixed
- **Stride nightly evaluation now targets yesterday** - The 03:00 nightly evaluation was evaluating "today" before the day had started, incorrectly marking sessions as missed. It now evaluates yesterday, the day that actually ended. (Hytte-y9xg)
- **Stride evaluation uses user notes for contextual assessment** - When a user writes notes targeting a specific date (e.g. "I'll skip today and do it tomorrow"), the nightly evaluation now sends those notes to Claude for a contextual evaluation instead of using generic template strings. (Hytte-y9xg)

category: Added
- **Date picker on Stride coach notes** - Notes now have a target date field so users can write notes about specific days. Defaults to today. The nightly evaluation picks up notes for the relevant date. (Hytte-y9xg)
