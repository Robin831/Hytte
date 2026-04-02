category: Fixed
- **Forge dashboard PR action buttons now send correct IPC format** - Fix Comments, Fix CI, Fix Conflicts, Bellows, Approve, and Merge buttons now send structured JSON pr_action commands to the daemon instead of plain text strings that were silently rejected. (Hytte-ki4n)
