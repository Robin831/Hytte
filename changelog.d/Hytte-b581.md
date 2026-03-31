category: Added
- **Forge SSE activity stream** - Added `GET /api/forge/activity/stream` endpoint that streams forge events to the browser via Server-Sent Events, polling the state database every 2 seconds and pushing new events as `data: <json>` frames. (Hytte-b581)
- **Forge worker log stream** - Added `GET /api/forge/workers/{id}/log` endpoint that streams a worker's log file via SSE, sending existing content on connect and tailing the file for new lines every 500 ms. (Hytte-b581)
