#!/usr/bin/env bash
set -euo pipefail
if ! state=$(systemctl is-active shop-worker.service); then
  echo "shop-worker is $state"
  exit 1
fi
