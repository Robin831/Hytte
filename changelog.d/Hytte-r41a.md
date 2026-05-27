category: Changed
- **Chat: update sidebar locally after send instead of refetching** - The send-message endpoint now returns the refreshed conversation (with auto-title applied synchronously) and the client merges it into local state, removing the follow-up `GET /api/chat/conversations` round-trip and the race with mid-flight conversation switches. (Hytte-r41a)
