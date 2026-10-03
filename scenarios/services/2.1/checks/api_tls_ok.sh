#!/usr/bin/env bash
# Connects the way the mobile app does: trusting only the system CA store,
# which has the internal root but not the intermediate.
set -euo pipefail
if ! out=$(curl -sS -o /dev/null -m 5 --resolve api.shop.internal:443:127.0.0.1 https://api.shop.internal/health 2>&1); then
  echo "${out%%$'\n'*}"
  exit 1
fi
