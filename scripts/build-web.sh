#!/usr/bin/env bash
# Build the web dashboard and bundle it into the Go binary at /ui.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> building Next.js static export"
(cd web && bun install --frozen-lockfile 2>/dev/null || bun install && bunx next build)

echo "==> bundling into internal/webui/dist"
rm -rf internal/webui/dist
cp -R web/out internal/webui/dist

echo "==> compiling evermemo with embedded dashboard"
go build -o evermemo .
echo "done: ./evermemo serve  ->  http://localhost:7777/ui/"
