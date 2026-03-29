category: Fixed
- **AI prompt save no longer fails with 500 errors** - Saving multiple AI prompts now fires requests sequentially instead of in parallel, avoiding SQLite write lock contention. (Hytte-7iu4)
