#!/usr/bin/env bash
#
# Build the single-binary chat-server with the production Vite SPA
# embedded under apps/server/internal/web/dist/.
#
# Order:
#   1. pnpm --filter web build  -> apps/web/dist/
#   2. clear apps/server/internal/web/dist/ except the tracked
#      placeholder index.html
#   3. copy apps/web/dist/* -> apps/server/internal/web/dist/
#      (which is exempted from the dist/ gitignore rule but otherwise
#      ignored — the copied assets stay untracked)
#   4. go build ./apps/server -o "$OUT"
#
# OUT defaults to ./bin/chat-server in the repo root. Pass an explicit
# output path as the first arg to override.
#
# This script is the canonical demo path; CI's plain `go build ./...`
# job only embeds the placeholder and is sufficient for the routing
# unit + e2e coverage. For an end-to-end "open the app in a browser"
# flow, run this script first.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

OUT="${1:-$ROOT/bin/chat-server}"
WEB_DIST="$ROOT/apps/web/dist"
EMBED_DIST="$ROOT/apps/server/internal/web/dist"

echo "[build-single-binary] pnpm --filter web build"
pnpm --filter web build

if [[ ! -f "$WEB_DIST/index.html" ]]; then
  echo "build-single-binary: $WEB_DIST/index.html missing after pnpm build" >&2
  exit 1
fi

echo "[build-single-binary] sync $WEB_DIST -> $EMBED_DIST"
# Wipe any stale untracked files (e.g. an old asset hash) but keep the
# tracked placeholder so the directory layout for `git status` matches
# what the .gitignore exception expects.
find "$EMBED_DIST" -mindepth 1 -not -name 'index.html' -delete 2>/dev/null || true

# Copy fresh assets. cp -R preserves the apps/web/dist/ tree shape
# (index.html, assets/*) inside the embed dir.
cp -R "$WEB_DIST/." "$EMBED_DIST/"

mkdir -p "$(dirname "$OUT")"

echo "[build-single-binary] go build -o $OUT ./apps/server"
go build -o "$OUT" ./apps/server

echo "[build-single-binary] done: $OUT"
