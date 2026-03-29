#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

# Send SIGTERM to any running ai-chat process (exact name match).
pkill -x "ai-chat" 2>/dev/null || true

# Wait for process to exit, timeout after 10 seconds.
timeout=10
while pgrep -x "ai-chat" >/dev/null 2>&1; do
    if (( timeout-- <= 0 )); then
        echo "Process did not exit within timeout, sending SIGKILL"
        pkill -9 -x "ai-chat" 2>/dev/null || true
        sleep 1
        break
    fi
    sleep 1
done

# Start new process.
# The flock provides a safety net: if the old process somehow lingers,
# the new one will fail-fast with a clear error instead of creating a conflict.
nohup ./ai-chat start >/dev/null 2>&1 &
echo "ai-chat started (PID: $!)"
