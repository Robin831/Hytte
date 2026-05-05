category: Added
- **New suggestion form** - The Suggestions page can now author user suggestions via a modal opened from the "+ New suggestion" button: page picker (loaded from `/api/suggestions/pages`), title (≤120 chars), body (≤4 KB), type, and size dropdowns with inline validation. Successful submission posts to `/api/suggestions` and refreshes the list. (Hytte-ym34)
