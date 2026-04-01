category: Fixed
- **Fix release version detection using wrong repository** - The release suggest endpoint now validates that the detected repository root contains go.mod, preventing stale version and changelog data when the server runs from a deployment directory instead of the source repo. The systemd service template now sets HYTTE_REPO_DIR explicitly. (Hytte-bc8g)
