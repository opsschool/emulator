#!/usr/bin/env bash
# shop.service has been running for at least four minutes.
set -euo pipefail
since=$(systemctl show -P ActiveEnterTimestampMonotonic shop.service)
now=$(awk '{printf "%d", $1 * 1000000}' /proc/uptime)
if ! systemctl is-active --quiet shop.service || ((now - since < 240000000)); then
  echo "shop.service has been up for $(((now - since) / 1000000))s"
  exit 1
fi
