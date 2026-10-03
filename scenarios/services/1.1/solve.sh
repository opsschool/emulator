#!/usr/bin/env bash
# Reference fix: correct the bad value in shop.env and restart both services.
set -euo pipefail
env_file=/etc/shop/shop.env
sed -i -e 's/^SHOP_DB_POOL_SIZE=4O$/SHOP_DB_POOL_SIZE=40/' \
  -e 's/^SHOP_CACHE_TTL=120$/SHOP_CACHE_TTL=120s/' \
  -e 's/^SHOP_CLIENT_KEEPALIVE=ture$/SHOP_CLIENT_KEEPALIVE=true/' \
  -e 's/^SHOP_DB_QUERY_TIMEOUT=10sec$/SHOP_DB_QUERY_TIMEOUT=10s/' "$env_file"
rm -rf /etc/systemd/system/shop.service.d
systemctl daemon-reload
systemctl reset-failed shop.service shop-worker.service || true
systemctl restart shop.service shop-worker.service
