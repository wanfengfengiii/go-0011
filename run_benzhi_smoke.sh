#!/usr/bin/env bash
set -euo pipefail

smoke_dir="$(mktemp -d "${TMPDIR:-/tmp}/concrete-smoke.XXXXXX")"
server_pid=""
port="${BENZHI_SMOKE_PORT:-19081}"
base_url="http://127.0.0.1:$port"

cleanup() {
  if [[ -n "$server_pid" ]] && kill -0 "$server_pid" 2>/dev/null; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -r -- "$smoke_dir"
}
trap cleanup EXIT INT TERM

GOPROXY=off go build -o "$smoke_dir/server" ./cmd/server

start_server() {
  LISTEN_ADDRESS="127.0.0.1:$port" \
  SQLITE_PATH="$smoke_dir/concrete.db" \
  CHECKPOINT_INTERVAL_SECONDS=1 \
    "$smoke_dir/server" >"$smoke_dir/server.log" 2>&1 &
  server_pid=$!
  for _ in {1..80}; do
    if health="$(curl --silent --show-error --max-time 1 "$base_url/health/ready" 2>/dev/null)"; then
      [[ "$health" == *'"ready":true'* ]] && return 0
    fi
    sleep 0.05
  done
  echo "service did not become ready" >&2
  return 1
}

start_server
created="$(curl --silent --show-error --fail --max-time 3 \
  -H 'Content-Type: application/json' \
  -d '{"id":"smoke-project","name":"Smoke Project","site_code":"SMOKE","created_at":"2026-01-01T00:00:00Z"}' \
  "$base_url/v1/projects")"
[[ "$created" == *'"id":"smoke-project"'* ]]

kill "$server_pid"
wait "$server_pid" || true
server_pid=""
start_server

status_file="$smoke_dir/status.txt"
status="$(curl --silent --show-error --max-time 3 \
  -o "$status_file" -w '%{http_code}' \
  -H 'Content-Type: application/json' \
  -d '{"id":"smoke-project","name":"Smoke Project","site_code":"SMOKE","created_at":"2026-01-01T00:00:00Z"}' \
  "$base_url/v1/projects")"
[[ "$status" == "409" ]]
conflict="$(<"$status_file")"
[[ "$conflict" == *'"code":"IDENTITY_CONFLICT"'* ]]

health="$(curl --silent --show-error --fail --max-time 3 "$base_url/health/ready")"
[[ "$health" == *'"phase":"ready"'* || "$health" == *'"phase":"ready_full_replay_digest_mismatch"'* ]]
echo "smoke passed: SQLite state survived restart and the HTTP API is ready"
