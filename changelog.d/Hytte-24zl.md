category: Fixed
- **Tool versions no longer show 'unavailable' under systemd** - The versions handler now checks ~/.local/bin and ~/bin as fallback paths when commands are not found in PATH, fixing the issue where the Hytte systemd service couldn't locate user-installed tools like forge, bd, and claude. (Hytte-24zl)
