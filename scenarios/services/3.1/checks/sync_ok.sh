#!/usr/bin/env bash
# The timer is still enabled and a sync run succeeds.
set -euo pipefail
if ! systemctl is-enabled --quiet config-sync.timer || ! systemctl is-active --quiet config-sync.timer; then
  echo "config-sync.timer is not enabled and running"
  exit 1
fi
systemctl start config-sync.service
