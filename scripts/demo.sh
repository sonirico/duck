#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

usage() {
  echo "usage: $0 {up|down}" >&2
  exit 1
}

demo_containers() {
  docker ps -a --format '{{.Names}}' | grep '^duck-demo-' || true
}

demo_down() {
  local names
  names=$(demo_containers)
  if [ -n "$names" ]; then
    echo "$names" | xargs -r docker rm -f >/dev/null
  fi
  docker network rm duck-demo-net >/dev/null 2>&1 || true
  docker volume rm duck-demo-data >/dev/null 2>&1 || true
}

demo_up() {
  demo_down

  docker network create duck-demo-net >/dev/null
  docker volume create duck-demo-data >/dev/null

  docker run -d --rm --name duck-demo-webshop-api \
    --network duck-demo-net \
    --label com.docker.compose.project=webshop \
    --label com.docker.compose.service=api \
    alpine sh -c '
      i=0
      while true; do
        i=$((i+1))
        codes="200 200 200 201 204 404 500"
        code=$(echo $codes | tr " " "\n" | shuf -n1)
        ms=$((RANDOM % 300 + 5))
        echo "{\"level\":\"info\",\"method\":\"GET\",\"path\":\"/api/orders/$i\",\"status\":$code,\"duration_ms\":$ms}"
        sleep 0.4
      done' >/dev/null

  docker run -d --rm --name duck-demo-webshop-worker \
    --label com.docker.compose.project=webshop \
    --label com.docker.compose.service=worker \
    alpine sh -c '
      jobs="send_email resize_image sync_inventory charge_card generate_invoice"
      while true; do
        job=$(echo $jobs | tr " " "\n" | shuf -n1)
        echo "[worker] processing job=$job queue=default attempt=1"
        sleep 0.7
        echo "[worker] job=$job done in $((RANDOM % 900 + 50))ms"
        sleep 0.6
      done' >/dev/null

  docker run -d --rm --name duck-demo-webshop-cache \
    --volume duck-demo-data:/data \
    --label com.docker.compose.project=webshop \
    --label com.docker.compose.service=cache \
    alpine sh -c '
      while true; do
        ops="GET SET DEL EXPIRE"
        op=$(echo $ops | tr " " "\n" | shuf -n1)
        echo "$(date +%H:%M:%S) $op key:session:$((RANDOM % 9999)) hit"
        sleep 0.3
      done' >/dev/null

  docker run -d --rm --name duck-demo-metrics-collector \
    --label com.docker.compose.project=metrics \
    --label com.docker.compose.service=collector \
    alpine sh -c '
      while true; do
        cpu=$((RANDOM % 100))
        mem=$((RANDOM % 100))
        echo "scrape target=node-exporter cpu=${cpu}% mem=${mem}% samples=42"
        sleep 0.8
      done' >/dev/null

  docker run -d --rm --name duck-demo-standalone \
    busybox sh -c '
      while true; do
        echo "standalone heartbeat ok"
        sleep 1
      done' >/dev/null
}

case "${1:-}" in
  up) demo_up ;;
  down) demo_down ;;
  *) usage ;;
esac
