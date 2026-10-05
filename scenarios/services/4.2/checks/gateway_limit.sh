#!/usr/bin/env bash
# The gateway still runs two authorizations at a time, at production
# latency: the card network's limit isn't the shop's to change.
set -euo pipefail
# The limit arrives with the break.
[[ "$OPSSCHOOL_PHASE" == baseline ]] && exit 0
cmd=$(systemctl show -p ExecStart --value shop-payments.service)
for want in '--workers 2 ' '--min-latency 100ms ' '--max-latency 180ms '; do
  if [[ $cmd != *"$want"* ]]; then
    echo "shop-payments runs without ${want% }"
    exit 1
  fi
done
