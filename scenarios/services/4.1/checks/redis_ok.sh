#!/usr/bin/env bash
# Redis can save, and still refuses writes if it ever can't.
set -euo pipefail
if [[ $(redis-cli config get stop-writes-on-bgsave-error | tail -1) != yes ]]; then
  echo "stop-writes-on-bgsave-error is off"
  exit 1
fi
before=$(redis-cli lastsave)
redis-cli bgsave >/dev/null 2>&1 || true
for _ in $(seq 30); do
  [[ $(redis-cli lastsave) != "$before" ]] && break
  sleep 1
done
if ! redis-cli info persistence | grep -q 'rdb_last_bgsave_status:ok'; then
  echo "Redis can't save its snapshot"
  exit 1
fi
