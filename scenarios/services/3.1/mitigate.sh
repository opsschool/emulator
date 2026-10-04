#!/usr/bin/env bash
# Reference mitigation: stop the sync, then fix the file on the server.
set -euo pipefail
systemctl disable --now config-sync.timer
sed -i 's|^SHOP_PAYMENTS_URL=.*|SHOP_PAYMENTS_URL=http://payments.shop.internal:8081|' /etc/shop/shop.env
systemctl restart shop.service shop-worker.service
