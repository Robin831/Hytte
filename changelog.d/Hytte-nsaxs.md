category: Changed
- **Family chat drops a half-typed edit when you switch conversations** - Message mutations (optimistic send/retry, inline edit, soft delete, reactions) moved into a dedicated `useMessageActions` hook; as part of that, an open inline editor or delete confirmation is now cleared on a conversation switch instead of carrying over into the new chat. (Hytte-nsaxs)
