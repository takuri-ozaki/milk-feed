#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

PID_FILE="milking.pid"
LOG_FILE="milking.log"

if [[ -f "$PID_FILE" ]] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
  echo "already running (pid=$(cat "$PID_FILE"))"
  exit 0
fi

go build -o milking ./...

nohup ./milking >>"$LOG_FILE" 2>&1 &
echo $! >"$PID_FILE"
disown

sleep 0.3
if kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
  echo "started (pid=$(cat "$PID_FILE")) http://localhost:8080/"
else
  echo "failed to start; see $LOG_FILE" >&2
  rm -f "$PID_FILE"
  exit 1
fi
