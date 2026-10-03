#!/usr/bin/env bash
# Reference fix: restore the index the query was written for.
set -euo pipefail
have=$(mysql -N -B shop -e "SELECT COUNT(*) FROM information_schema.statistics
  WHERE table_schema = 'shop' AND table_name = 'orders' AND index_name = 'idx_orders_customer'")
if ((have > 0)); then
  mysql shop -e 'ALTER TABLE orders DROP INDEX idx_orders_customer'
fi
mysql shop -e 'ALTER TABLE orders ADD INDEX idx_orders_customer_created (customer_id, created_at)'
