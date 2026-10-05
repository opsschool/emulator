#!/usr/bin/env bash
# Reference fix: roll back to 2.3.0, whose worker commits its reconcile
# transaction. Restarting the worker closes the idle transactions, and the
# migration then completes at once.
set -euo pipefail
ln -sfn /opt/shop/releases/2.3.0 /opt/shop/current
systemctl restart shop-worker.service shop.service
mig=$(ls /opt/shop/migrations/*_orders_add_gift_note.sql)
if ! mysql -N -B -e "SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'shop' AND table_name = 'orders' AND column_name = 'gift_note'" | grep -q 1; then
  mysql shop <"$mig"
fi
