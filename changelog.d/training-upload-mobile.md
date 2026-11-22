category: Fixed
- **Workout upload from a phone** - Picked .fit files are read into memory before sending, so files handed over by another app (Drive, Garmin, Downloads) no longer fail mid-upload; a dropped connection is retried once; and the error now says whether the file could not be read, Hytte could not be reached, or the request was blocked on the way.
