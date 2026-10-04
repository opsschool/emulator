#!/usr/bin/env bash
# Reference fix: clean up, and make the shop and worker require /data.
set -euo pipefail
bash "$(dirname "$0")/mitigate.sh"
for unit in shop shop-worker; do
  install -d "/etc/systemd/system/$unit.service.d"
  printf '[Unit]\nRequiresMountsFor=/data\n' >"/etc/systemd/system/$unit.service.d/data.conf"
done
systemctl daemon-reload
