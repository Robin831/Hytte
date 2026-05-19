category: Added
- **Family Chat live updates over SSE** - The chat view now subscribes to the conversation's SSE stream so new messages appear in real time. On reconnect after a dropped stream, the client gap-fills via `GET /messages?since=…` so no messages are lost while disconnected. (Hytte-c0cw)
