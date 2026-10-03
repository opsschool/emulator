#!/usr/bin/env bash
# A schema cleanup drops an index that an index-usage report (taken on a
# replica that never served that query) said was unused. The customer order
# history query now scans the whole orders table.
set -euo pipefail

name="${OPSSCHOOL_VAR_MIGRATION}_drop_unused_orders_index"
dir=/opt/shop/migrations
mkdir -p "$dir"
cat >"$dir/$name.sql" <<'SQL'
-- Cleanup: indexes with zero reads in last week's index-usage report.
ALTER TABLE orders DROP INDEX idx_orders_customer_created;
SQL
mysql shop <"$dir/$name.sql"
printf '%s applied %s\n' "$(date -u +%FT%TZ)" "$name" >>/var/log/shop-migrate.log
