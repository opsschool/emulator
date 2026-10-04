#!/usr/bin/env bash
# The shop's configured CPU limits, on its unit and every slice above it,
# allow at least one full CPU. Reads systemd's configuration rather than the
# live cgroup, so a change made only in /sys/fs/cgroup doesn't count.
set -euo pipefail
unit=shop.service
while [[ -n $unit && $unit != -.slice ]]; do
  q=$(systemctl show -P CPUQuotaPerSecUSec -- "$unit")
  if [[ $q != infinity ]]; then
    us=$(systemd-analyze timespan "$q" | awk '/μs:|us:/ {print $2}')
    if ((us < 1000000)); then
      echo "$unit is limited to $q of CPU per second"
      exit 1
    fi
  fi
  unit=$(systemctl show -P Slice -- "$unit")
done
