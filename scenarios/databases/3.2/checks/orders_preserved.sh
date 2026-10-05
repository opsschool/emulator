#!/usr/bin/env bash
# The order count must never drop below what it was before the break. The
# count waits for the metadata lock like any query on orders, so give up
# after a while rather than hang while the incident is still on.
set -euo pipefail
state="$OPSSCHOOL_STATE_DIR/orders_baseline"
count=$(mysql -N -B shop -e 'SET SESSION lock_wait_timeout = 20; SELECT COUNT(*) FROM orders')
if [[ "$OPSSCHOOL_PHASE" == baseline ]]; then
  echo "$count" >"$state"
  exit 0
fi
((count >= $(cat "$state")))
