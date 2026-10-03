#!/usr/bin/env bash
# The order count must never drop below what it was before the break.
set -euo pipefail
state="$OPSSCHOOL_STATE_DIR/orders_baseline"
count=$(mysql -N -B shop -e 'SELECT COUNT(*) FROM orders')
if [[ "$OPSSCHOOL_PHASE" == baseline ]]; then
  echo "$count" >"$state"
  exit 0
fi
((count >= $(cat "$state")))
