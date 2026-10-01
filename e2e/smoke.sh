#!/usr/bin/env bash
# Smoke test for the cassandrasafe image.
#
# The script starts a two-node Cassandra cluster from a compose file, then
# runs the image as a sidecar of node1 three times:
#   1. Both nodes answer. The sidecar must exit 0.
#   2. node2 is paused. The sidecar must keep waiting until the timeout ends.
#   3. node2 answers again. The sidecar must exit 0.
#
# Usage: e2e/smoke.sh IMAGE COMPOSE_FILE
set -euo pipefail

IMAGE=${1:?usage: e2e/smoke.sh IMAGE COMPOSE_FILE}
COMPOSE_FILE=${2:?usage: e2e/smoke.sh IMAGE COMPOSE_FILE}

NODE1=cassandrasafe-e2e-node1
NODE2=cassandrasafe-e2e-node2
SIDECAR=cassandrasafe-e2e-sidecar
SIDECAR_LOG=$(mktemp)

if command -v timeout >/dev/null 2>&1; then
  TIMEOUT=timeout
elif command -v gtimeout >/dev/null 2>&1; then
  TIMEOUT=gtimeout
else
  echo "GNU timeout is required (install coreutils on macOS)" >&2
  exit 1
fi

log() {
  printf '[smoke] %s\n' "$*"
}

fail() {
  echo "[smoke] FAIL: $*" >&2
  echo "--- sidecar output ---" >&2
  cat "$SIDECAR_LOG" >&2
  exit 1
}

compose() {
  docker compose -f "$COMPOSE_FILE" "$@"
}

cleanup() {
  local status=$?
  if [ "$status" -ne 0 ]; then
    echo "--- cluster logs ---" >&2
    compose logs --tail 100 >&2 || true
  fi
  docker rm -f "$SIDECAR" >/dev/null 2>&1 || true
  docker unpause "$NODE2" >/dev/null 2>&1 || true
  compose down -v --remove-orphans >/dev/null 2>&1 || true
  rm -f "$SIDECAR_LOG"
  exit "$status"
}
trap cleanup EXIT

# run_sidecar runs the image as a sidecar of node1 with a time limit in
# seconds. It stores the output in SIDECAR_LOG and returns the exit status.
# GNU timeout returns 124 when the limit ends the run.
run_sidecar() {
  local seconds=$1
  shift
  local status=0
  "$TIMEOUT" -k 10 "$seconds" \
    docker run --rm --name "$SIDECAR" --network "container:$NODE1" "$IMAGE" "$@" \
    >"$SIDECAR_LOG" 2>&1 || status=$?
  return "$status"
}

log "starting the cluster from $COMPOSE_FILE"
compose down -v --remove-orphans >/dev/null 2>&1 || true
compose up -d --wait --wait-timeout 600

log "waiting for both nodes to join the ring"
deadline=$((SECONDS + 300))
until [ "$(docker exec "$NODE1" nodetool status 2>/dev/null | grep -c '^UN')" -eq 2 ]; do
  if [ "$SECONDS" -ge "$deadline" ]; then
    echo "[smoke] FAIL: the nodes did not join the ring in time" >&2
    exit 1
  fi
  sleep 5
done
NODE2_IP=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$NODE2")
log "node2 address is $NODE2_IP"

log "case 1: both nodes answer, the sidecar must exit 0"
status=0
run_sidecar 300 --pass-interval=1s || status=$?
[ "$status" -eq 0 ] || fail "exit status $status, expected 0"
grep -q 'steady state reached' "$SIDECAR_LOG" || fail "the steady state message is missing"

log "case 2: node2 is paused, the sidecar must keep waiting"
docker pause "$NODE2"
status=0
run_sidecar 30 || status=$?
[ "$status" -eq 124 ] || fail "exit status $status, expected 124 (time limit)"
grep -q 'waiting for hosts' "$SIDECAR_LOG" || fail "the waiting message is missing"
grep -q "$NODE2_IP" "$SIDECAR_LOG" || fail "node2 ($NODE2_IP) is not listed as outstanding"
docker unpause "$NODE2"

log "case 3: node2 answers again, the sidecar must exit 0"
status=0
run_sidecar 300 --pass-interval=1s || status=$?
[ "$status" -eq 0 ] || fail "exit status $status, expected 0"
grep -q 'steady state reached' "$SIDECAR_LOG" || fail "the steady state message is missing"

log "PASS: $COMPOSE_FILE"
