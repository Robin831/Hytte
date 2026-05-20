category: Added
- **Family Chat: edit + soft-delete tombstones** - Own messages now have an actions menu with Edit (inline editor + "(edited)" tag) and Delete (confirm modal). Deletes are soft, keeping the message's position in history as a tombstone bubble; other clients see the change live over SSE via the new `message_edited` and extended `message_deleted` events. (Hytte-f0tw)
