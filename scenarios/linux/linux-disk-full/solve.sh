#!/usr/bin/env bash
# Reference fix: restore the log level and log file, rotate logs, free space.
set -euo pipefail
source /etc/shop/shop.env
old_log="$SHOP_LOG_FILE"
sed -i -e 's/^SHOP_LOG_LEVEL=.*/SHOP_LOG_LEVEL=info/' \
  -e 's|^SHOP_LOG_FILE=.*|SHOP_LOG_FILE=/data/log/shop/app.log|' /etc/shop/shop.env
cat >/etc/logrotate.d/shop <<'CONF'
/data/log/shop/*.log {
    daily
    rotate 7
    maxsize 100M
    compress
    missingok
    notifempty
    copytruncate
}
CONF
if [[ "$old_log" != /data/log/shop/app.log ]]; then
  rm -f "$old_log"
fi
systemctl restart mysql.service shop.service
