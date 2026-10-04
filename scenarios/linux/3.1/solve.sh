#!/usr/bin/env bash
# Reference fix: clean up under the mountpoint, and make the shop and worker
# require /data.
set -euo pipefail
dir=$(mktemp -d)
mount --bind / "$dir"
rm -rf "${dir:?}/data/log" "${dir:?}/data/sessions"
umount "$dir"
rmdir "$dir"
for unit in shop shop-worker; do
  install -d "/etc/systemd/system/$unit.service.d"
  printf '[Unit]\nRequiresMountsFor=/data\n' >"/etc/systemd/system/$unit.service.d/data.conf"
done
systemctl daemon-reload
