#!/usr/bin/env bash
# shop-thumbs is running and has not restarted for 20 seconds.
set -euo pipefail
before=$(systemctl show -P NRestarts shop-thumbs.service)
sleep 20
after=$(systemctl show -P NRestarts shop-thumbs.service)
if ! systemctl is-active --quiet shop-thumbs.service || [[ $before != "$after" ]]; then
  echo "shop-thumbs is not running steadily (restarts $before -> $after)"
  exit 1
fi
