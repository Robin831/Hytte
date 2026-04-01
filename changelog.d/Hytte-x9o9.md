category: Added
- **Parsed worker log endpoint** - New `GET /api/forge/workers/{id}/log/parsed` endpoint that reads a worker's stream-json log file and returns a structured JSON array of `LogEntry` objects (type, name, content, status). Tool results are correlated back to their `tool_use` entries by `tool_use_id` to set success/error status and enrich content with a result summary. (Hytte-x9o9)
