category: Fixed
- **Stride coach chat "network error" on slow replies** - The chat now sends a periodic SSE keepalive while waiting on Claude, so a slow first response (e.g. resuming a long conversation) no longer leaves the connection idle long enough for mobile browsers/proxies to drop it — which previously surfaced as a "network error" around the 2-minute mark.
