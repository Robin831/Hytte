#!/usr/bin/env bash
# Start a throwaway Hytte instance for UI screenshots, wired to a COPY of the
# production database with an injected session for user id 1, so pages render
# with real data (live Wordfeud games, real lists) without touching prod.
#
# Usage:
#   tools/ui-preview/preview.sh [port]        # default 8095 (8090 is forge's)
#
# It builds nothing — build the frontend first (cd web && npm run build) and
# the server binary is built here from the current checkout. Serves web/dist
# of THIS checkout, so screenshots reflect your working tree.
#
# Cleanup when done: kill the server and delete $PREVIEW_DIR (it contains a
# DB copy and a valid session token for it).
set -euo pipefail

PORT="${1:-8095}"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
PREVIEW_DIR="${PREVIEW_DIR:-/tmp/hytte-ui-preview}"
PROD_DB="${PROD_DB:-$HOME/Hytte/hytte.db}"

mkdir -p "$PREVIEW_DIR/web" "$PREVIEW_DIR/data"

# Fresh DB copy (checkpoint WAL first so the copy is complete).
sqlite3 "$PROD_DB" "PRAGMA wal_checkpoint(TRUNCATE);" >/dev/null
cp -f "$PROD_DB" "$PREVIEW_DIR/hytte.db"

# Inject a session for user 1 (cookie name: session; DB stores sha256(token)).
TOKEN=$(head -c32 /dev/urandom | xxd -p -c64)
HASH=$(printf %s "$TOKEN" | sha256sum | cut -d' ' -f1)
sqlite3 "$PREVIEW_DIR/hytte.db" \
  "INSERT INTO sessions (token, user_id, expires_at) VALUES ('$HASH', 1, datetime('now', '+1 day'));"
printf %s "$TOKEN" > "$PREVIEW_DIR/token"
chmod 600 "$PREVIEW_DIR/token"

# Static files + wordfeud dictionary from the checkout / prod data dir.
ln -sfn "$REPO_ROOT/web/dist" "$PREVIEW_DIR/web/dist"
ln -sfn "$HOME/Hytte/data/nsf2025.txt" "$PREVIEW_DIR/data/nsf2025.txt"

echo "Building server binary..."
go -C "$REPO_ROOT" build -o "$PREVIEW_DIR/hytte-preview" ./cmd/server

echo "Preview on http://localhost:$PORT — session token in $PREVIEW_DIR/token"
cd "$PREVIEW_DIR"
PORT="$PORT" exec ./hytte-preview
