#!/usr/bin/env bash
# Reference fix: remove the quota from the slice's unit file. The slice can
# stay for cost reporting.
set -euo pipefail
sed -i '/^CPUQuota=/d' "/etc/systemd/system/$OPSSCHOOL_VAR_SLICE.slice"
systemctl daemon-reload
systemctl restart "$OPSSCHOOL_VAR_SLICE.slice" shop.service shop-worker.service
