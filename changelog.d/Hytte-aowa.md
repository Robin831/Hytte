category: Fixed
- **Wordfeud solver: prevent invalid words from appending to existing board words** - Fixed slice aliasing in the solver's extendRight function by using explicit copies instead of Go's append (which can share underlying arrays across recursive branches). Added a post-generation dictionary validation filter as a safety net to ensure no invalid word is ever returned. (Hytte-aowa)
