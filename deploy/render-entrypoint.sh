#!/bin/sh
set -eu

if [ -z "${DATABASE_URL:-}" ]; then
  echo "DATABASE_URL is required" >&2
  exit 1
fi

export PORT="${PORT:-10000}"
export RELAY_HTTP_ADDR="127.0.0.1:8080"

/app/goose -dir /app/migrations postgres "$DATABASE_URL" up

/app/nats-server --jetstream --store_dir=/tmp/relay-nats --addr=127.0.0.1 --port=4222 &
nats_pid=$!
/app/relay-api &
api_pid=$!
/app/relay-controlplane &
controlplane_pid=$!
/app/relay-worker &
worker_pid=$!
cd /app/web
node server.js &
web_pid=$!

shutdown() {
  trap - TERM INT EXIT
  kill -TERM "$web_pid" "$worker_pid" "$controlplane_pid" "$api_pid" "$nats_pid" 2>/dev/null || true
  wait "$web_pid" "$worker_pid" "$controlplane_pid" "$api_pid" "$nats_pid" 2>/dev/null || true
}
trap shutdown TERM INT EXIT

while kill -0 "$web_pid" 2>/dev/null; do
  for service_pid in "$nats_pid" "$api_pid" "$controlplane_pid" "$worker_pid"; do
    if ! kill -0 "$service_pid" 2>/dev/null; then
      echo "A Relay runtime process exited unexpectedly" >&2
      exit 1
    fi
  done
  sleep 2
done

wait "$web_pid"
