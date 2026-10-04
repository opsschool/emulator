#!/usr/bin/env bash
# The configured task limit is at least 512, or unlimited.
set -euo pipefail
max=$(systemctl show -P TasksMax shop.service)
if [[ $max != infinity ]] && ((max < 512)); then
  echo "shop.service TasksMax is $max"
  exit 1
fi
