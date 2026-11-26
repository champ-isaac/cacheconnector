#!/usr/bin/env bash
# Verification helper for cacheconnector.
#   verify.sh up       start redis, valkey, memcached from docker-compose.yaml (only those not already running)
#   verify.sh doctor   open + Ping + one round trip per backend (read-mostly)
#   verify.sh drive    run the consumer journeys, write the transcript to the evidence dir
#   verify.sh down     stop only the containers this run started; evidence is kept
#   verify.sh all      up, doctor, drive, down (down runs even on failure)
set -u
REPO="$(cd "$(dirname "$0")/../../../.." && pwd)"
EVROOT="$REPO/.claude/verify-evidence"
mkdir -p "$EVROOT"
RUN_ID="${RUN_ID:-$(cat "$EVROOT/.last-run" 2>/dev/null || date +%Y%m%d-%H%M%S)}"
EV="$EVROOT/$RUN_ID"
SERVICES="redis valkey memcached"   # redis-ui is not needed
mkdir -p "$EV"
echo "$RUN_ID" > "$EVROOT/.last-run"
cd "$REPO"

driver() { go run ./.claude/skills/verify-cacheconnector/driver -mode "$1" -out "$EV" -run "$RUN_ID"; }

up() {
  : > "$EV/started"
  for s in $SERVICES; do
    if [ "$(docker inspect -f '{{.State.Running}}' "$s" 2>/dev/null)" = "true" ]; then
      echo "$s already running, will leave it running"
    else
      docker compose up -d "$s" && echo "$s" >> "$EV/started"
    fi
  done
  # Ready = usable: poll the doctor until every backend answers a round trip.
  for i in $(seq 1 30); do
    if driver doctor >"$EV/doctor.txt" 2>&1; then echo "ready after ${i} try(ies)"; return 0; fi
    sleep 1
  done
  cat "$EV/doctor.txt"; echo "backends not ready"; return 1
}

down() {
  [ -s "$EV/started" ] || { echo "nothing started by this run"; return 0; }
  # shellcheck disable=SC2046
  docker compose stop $(cat "$EV/started") && echo "stopped: $(tr '\n' ' ' < "$EV/started")"
}

case "${1:-}" in
  up) up ;;
  doctor) driver doctor 2>&1 | tee "$EV/doctor.txt"; exit "${PIPESTATUS[0]}" ;;
  drive) driver drive 2>&1 | tee "$EV/transcript.txt"; exit "${PIPESTATUS[0]}" ;;
  down) down ;;
  all)
    rc=0
    up || rc=1
    if [ $rc -eq 0 ]; then driver drive 2>&1 | tee "$EV/transcript.txt"; rc=${PIPESTATUS[0]}; fi
    down
    echo "evidence: $EV"
    exit $rc ;;
  *) sed -n 2,7p "$0"; exit 2 ;;
esac
