# CLAUDE.md

## Build & Run

```bash
# Build the backend
make build

# Run the backend (default port 8080)
make run

# Run with custom port
PORT=3000 go run ./cmd/server

# Run tests
make test

# Clean build artifacts
make clean
```

## Project Structure

- **Backend**: Go server in `cmd/server/` with Chi router and SQLite (CGO-free via modernc.org/sqlite)
- **Frontend**: React + TypeScript + Vite app in `web/` (served as static files from `web/dist/`)

## API

All API routes are prefixed with `/api/`. Non-API routes fall through to the SPA frontend.

- `GET /api/health` — Health check

## Database

SQLite with WAL mode. The database file (`hytte.db`) is created automatically on first run.

## Shell Safety (on Windows)

Always use non-interactive flags to avoid hanging on prompts:
```bash
cp -f source dest
rm -f file
rm -rf dir
```
