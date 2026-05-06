category: Added
- **Streaming progress UI on Suggestions page** - Run now consumes the SSE event stream from `POST /api/suggestions/run`: a header progress pill and per-page log update live, the Pending tab refetches between events, a 409 response surfaces a banner pointing to recent runs, and a Reconnect button appears if the stream drops mid-run. (Hytte-39fp)
