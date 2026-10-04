#!/usr/bin/env bash
# shop.service and shop-worker.service must require /data, so they never run
# with the volume missing.
set -euo pipefail
for unit in shop.service shop-worker.service; do
  needs=$(systemctl show -P RequiresMountsFor "$unit")
  reqs=$(systemctl show -P Requires "$unit")
  binds=$(systemctl show -P BindsTo "$unit")
  if [[ " $needs " != *" /data "* && " $reqs $binds " != *" data.mount "* ]]; then
    echo "$unit does not require /data"
    exit 1
  fi
done
