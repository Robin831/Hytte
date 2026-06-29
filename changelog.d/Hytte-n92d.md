category: Fixed
- **Family chat message deduplication is now order-independent** - When a sent message arrived via both the POST response and the SSE `message_new` broadcast, a race could leave two identical bubbles if the SSE event was processed before the POST resolved. Reconciling an optimistic bubble now also drops any duplicate row sharing the same server id. (Hytte-n92d)
