#!/usr/bin/env bash
# A cost-control change groups the shop and its worker into one systemd
# slice and caps the slice's CPU. The host stays mostly idle, but the shop
# is throttled whenever the slice uses up its quota, which it does at any
# real traffic: the quota was sized from the average, and the worker shares
# it.
set -euo pipefail

slice="$OPSSCHOOL_VAR_SLICE.slice"
cat >"/etc/systemd/system/$slice" <<UNIT
[Unit]
Description=Application services (cost allocation, FIN-207)

[Slice]
# Sized from last month's average CPU use (2%) plus headroom (FIN-207).
CPUQuota=${OPSSCHOOL_VAR_QUOTA}%
UNIT
for unit in shop shop-worker; do
  install -d "/etc/systemd/system/$unit.service.d"
  cat >"/etc/systemd/system/$unit.service.d/50-cost-allocation.conf" <<UNIT
# Cost allocation (FIN-207): application services share one slice.
[Service]
Slice=$slice
UNIT
done
systemctl daemon-reload
systemctl restart shop.service shop-worker.service
logger -t finops-rollout "FIN-207: moved shop.service, shop-worker.service to $slice"
