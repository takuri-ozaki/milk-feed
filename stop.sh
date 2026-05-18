#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

PID_FILE="milking.pid"

pid=""
if [[ -f "$PID_FILE" ]]; then
  pid="$(cat "$PID_FILE")"
fi

if [[ -z "$pid" ]] || ! kill -0 "$pid" 2>/dev/null; then
  pid="$(lsof -i :8080 -t 2>/dev/null | head -n1 || true)"
fi

if [[ -z "$pid" ]]; then
  echo "not running"
  rm -f "$PID_FILE"
  exit 0
fi

kill "$pid"
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if ! kill -0 "$pid" 2>/dev/null; then
    break
  fi
  sleep 0.2
done

if kill -0 "$pid" 2>/dev/null; then
  kill -9 "$pid"
fi

rm -f "$PID_FILE"
echo "stopped (pid=$pid)"
