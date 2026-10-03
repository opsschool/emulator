#!/usr/bin/env bash
# Passes when the API and the worker are running and read their settings
# from /etc/shop/shop.env, not from a copy.
set -euo pipefail
for unit in shop.service shop-worker.service; do
  systemctl is-active --quiet "$unit" || { echo "$unit is not running"; exit 1; }
  files=$(systemctl show -p EnvironmentFiles --value "$unit")
  [[ "$files" == "/etc/shop/shop.env "* || "$files" == /etc/shop/shop.env ]] ||
    { echo "$unit reads $files"; exit 1; }
done
