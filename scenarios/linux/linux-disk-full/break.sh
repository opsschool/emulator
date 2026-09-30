#!/usr/bin/env bash
# Someone turned on debug logging to chase a bug and forgot about it. The
# verbose log fills the data volume, and MySQL can no longer write orders.
set -euo pipefail

log_dir=/data/log/shop
log_file="$log_dir/$OPSSCHOOL_VAR_LOG_NAME"
env_file=/etc/shop/shop.env

sed -i -e 's/^SHOP_LOG_LEVEL=.*/SHOP_LOG_LEVEL=debug/' \
  -e "s|^SHOP_LOG_FILE=.*|SHOP_LOG_FILE=$log_file|" "$env_file"
systemctl restart shop.service

# Fast-forward a few hours of debug logging: fill the volume with realistic
# lines until only the headroom is left.
free_mb=$(df --output=avail -m /data | tail -1 | tr -d ' ')
fill_mb=$((free_mb - OPSSCHOOL_VAR_HEADROOM_MB))
if ((fill_mb > 0)); then
  awk -v mb="$fill_mb" -v seed="$OPSSCHOOL_SEED" 'BEGIN {
    srand(seed); limit = mb * 1048576; n = 0
    split("GET /products,GET /products/%d,POST /orders,GET /orders/%d", routes, ",")
    while (n < limit) {
      r = routes[int(rand() * 4) + 1]
      line = sprintf("{\"level\":\"debug\",\"msg\":\"request\",\"route\":\"%s\",\"cache\":\"%s\",\"db_ms\":%.3f,\"headers\":{\"user-agent\":\"shop-web/2.3\",\"accept\":\"application/json\"},\"trace_id\":\"%08x%08x\"}\n", sprintf(r, int(rand() * 5000)), rand() < 0.8 ? "hit" : "miss", rand() * 20, rand() * 4294967295, rand() * 4294967295)
      printf "%s", line
      n += length(line)
    }
  }' >>"$log_file" || true
fi
chown shop:shop "$log_file"
