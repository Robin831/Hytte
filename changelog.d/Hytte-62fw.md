category: Changed
- **Refactored the Notes page internals** - Extracted the notes data layer into a `useNotes` hook and split the editor pane into a `NoteEditor` subcomponent, leaving `Notes.tsx` as a thin list/search/tag shell. No user-visible behavior changes. (Hytte-62fw)
