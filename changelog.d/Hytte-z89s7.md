category: Added
- **Rename and delete homework conversations** - Each row on the homework list now has a keyboard-accessible overflow menu with inline rename and a delete confirmation, backed by new `PATCH` and `DELETE /api/homework/conversations/{id}` endpoints. Deleting removes the conversation, its messages and its uploaded images. (Hytte-z89s7)
