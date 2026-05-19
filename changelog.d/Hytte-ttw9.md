category: Added
- **Family Chat frontend** - New `/familychat` page combining a conversation sidebar, chat view with header/messages/composer, and a new-conversation modal. Messages stream in live over SSE and gap-fill on reconnect so nothing is lost while disconnected. Covered by integration tests for initial render, sending, SSE message receipt, and the new-conversation flow. (Hytte-ttw9)
