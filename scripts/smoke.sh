#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

cleanup() {
  docker rm -f duck-smoke-api duck-smoke-db duck-smoke-loner >/dev/null 2>&1 || true
  docker volume rm duck-smoke-vol >/dev/null 2>&1 || true
  docker network rm duck-smoke-net >/dev/null 2>&1 || true
}
trap cleanup EXIT

go build -o duck .

docker run -d --rm --name duck-smoke-api \
  --label com.docker.compose.project=duck-smoke \
  --label com.docker.compose.service=api \
  alpine sh -c 'while true; do echo "api tick"; sleep 1; done' >/dev/null

docker run -d --rm --name duck-smoke-db \
  --label com.docker.compose.project=duck-smoke \
  --label com.docker.compose.service=db \
  alpine sh -c 'while true; do echo "db tick"; sleep 1; done' >/dev/null

docker run -d --rm --name duck-smoke-loner alpine sleep 60 >/dev/null

docker volume create duck-smoke-vol >/dev/null
docker network create duck-smoke-net >/dev/null

OUT=$(mktemp)
python3 scripts/ptyrun.py ./duck "$OUT" 5

VOLOUT=$(mktemp)
python3 scripts/ptyrun.py ./duck "$VOLOUT" 5 2

NETOUT=$(mktemp)
python3 scripts/ptyrun.py ./duck "$NETOUT" 5 3

docker rm -f duck-smoke-api duck-smoke-db duck-smoke-loner >/dev/null 2>&1 || true
docker volume rm duck-smoke-vol >/dev/null 2>&1 || true
docker network rm duck-smoke-net >/dev/null 2>&1 || true

stack=false
api=false
db=false
logs_title=false
volume_tab=false
network_tab=false

if grep -aq "duck-smoke" "$OUT"; then
  stack=true
fi
if grep -aq "api" "$OUT"; then
  api=true
fi
if grep -aq "db" "$OUT"; then
  db=true
fi
if grep -aq "logs:" "$OUT"; then
  logs_title=true
fi
if grep -aq "duck-smoke-vol" "$VOLOUT"; then
  volume_tab=true
fi
if grep -aq "duck-smoke-net" "$NETOUT"; then
  network_tab=true
fi

sha=$(git rev-parse HEAD)
receipt_dir=".claude/receipts/$sha"
mkdir -p "$receipt_dir"

ok=false
if [ "$stack" = true ] && [ "$api" = true ] && [ "$db" = true ] && [ "$logs_title" = true ] && [ "$volume_tab" = true ] && [ "$network_tab" = true ]; then
  ok=true
fi

cat > "$receipt_dir/smoke.json" <<EOF
{"ok":$ok,"sha":"$sha","checks":{"stack":$stack,"api":$api,"db":$db,"logs_title":$logs_title,"volume_tab":$volume_tab,"network_tab":$network_tab}}
EOF

if [ "$ok" = true ]; then
  exit 0
else
  exit 1
fi
