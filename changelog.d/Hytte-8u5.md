category: Fixed
- **AI-generated workout titles now apply correctly** - The "Analyze with Claude" feature was not updating workout titles because the condition was too restrictive: it required both title and title_source to be empty, but .fit imports set title_source to 'device'. Relaxed the condition to allow AI to overwrite any title except user-set ones. (Hytte-8u5)
