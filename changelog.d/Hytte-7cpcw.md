category: Fixed
- **Backdated imports land in the right place when refreshing the training list** - Clicking "New workouts available" now folds the fetched page into the list in `started_at` order instead of prepending it, so a backdated .fit import (or an edited start time) appears in its chronological slot, edits replace the existing row, and workouts deleted elsewhere disappear. (Hytte-7cpcw)
