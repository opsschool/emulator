#!/usr/bin/env bash
# systemd-coredump keeps some of the disk free.
set -euo pipefail
keep=$(cat /etc/systemd/coredump.conf /etc/systemd/coredump.conf.d/*.conf 2>/dev/null |
  sed -n 's/^ *KeepFree *= *//p' | tail -1)
if [[ $keep == 0 || $keep == 0[A-Za-z]* ]]; then
  echo "core dumps are allowed to use all of the disk (KeepFree=$keep)"
  exit 1
fi
