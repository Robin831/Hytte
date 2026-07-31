category: Fixed
- **Notes warns before losing unsaved edits on refresh or navigation** - The discard-changes guard on the Notes page now also covers browser refresh/tab close (native "leave site?" prompt) and in-app route changes such as sidebar links and browser back/forward, which previously discarded a dirty draft silently. (Hytte-4dc7h)
