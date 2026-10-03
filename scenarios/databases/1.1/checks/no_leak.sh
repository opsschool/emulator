#!/usr/bin/env bash
# Sends orders for products that do not exist, which the leaky build never
# rolls back, then counts the shop's idle connections still holding a
# transaction open.
set -euo pipefail
for _ in $(seq 20); do
  curl -s -o /dev/null -m 5 -X POST -H 'Content-Type: application/json' \
    -d '{"customer_id":1,"product_id":999999,"quantity":1}' http://127.0.0.1:8080/orders || true
done
sleep 3
open=$(mysql -N -B -e "
  SELECT COUNT(*) FROM information_schema.innodb_trx t
  JOIN performance_schema.threads th ON th.PROCESSLIST_ID = t.trx_mysql_thread_id
  WHERE th.PROCESSLIST_USER = 'shop' AND th.PROCESSLIST_COMMAND = 'Sleep'
    AND t.trx_started < NOW() - INTERVAL 2 SECOND")
echo "$open idle transactions held open by the shop"
((open < 3))
