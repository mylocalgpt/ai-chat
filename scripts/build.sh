#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

echo "Building Go binary..."
go build -o ai-chat ./cmd/ai-chat/

echo "Done: ./ai-chat"
