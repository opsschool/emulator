#!/usr/bin/env bash
# A config change bumps one setting and gets the value wrong. The shop and
# the worker both read shop.env, refuse to start, and systemd keeps
# restarting them.
set -euo pipefail

env_file=/etc/shop/shop.env
# The change tool keeps a copy of the previous config.
cp -p "$env_file" "$env_file.bak"

case "$OPSSCHOOL_VAR_SETTING" in
  SHOP_DB_POOL_SIZE) sed -i 's/^SHOP_DB_POOL_SIZE=.*/SHOP_DB_POOL_SIZE=4O/' "$env_file" ;;
  SHOP_CACHE_TTL) sed -i 's/^SHOP_CACHE_TTL=.*/SHOP_CACHE_TTL=120/' "$env_file" ;;
  SHOP_CLIENT_KEEPALIVE) sed -i 's/^SHOP_CLIENT_KEEPALIVE=.*/SHOP_CLIENT_KEEPALIVE=ture/' "$env_file" ;;
  SHOP_DB_QUERY_TIMEOUT) sed -i 's/^SHOP_DB_QUERY_TIMEOUT=.*/SHOP_DB_QUERY_TIMEOUT=10sec/' "$env_file" ;;
  *)
    echo "unknown setting $OPSSCHOOL_VAR_SETTING" >&2
    exit 1
    ;;
esac
systemctl restart shop.service shop-worker.service || true
