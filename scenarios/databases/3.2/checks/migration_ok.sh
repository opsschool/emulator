#!/usr/bin/env bash
# Passes when orders has the gift_note column.
set -euo pipefail
mysql -N -B -e "SELECT 1 FROM information_schema.columns
  WHERE table_schema = 'shop' AND table_name = 'orders' AND column_name = 'gift_note'" | grep -q 1
