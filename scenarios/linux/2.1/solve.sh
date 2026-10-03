#!/usr/bin/env bash
# Reference fix.
set -euo pipefail
case "$OPSSCHOOL_VAR_VARIANT" in
  deleted-log)
    # Rotate the shop's logs without leaving it writing to a deleted file,
    # and restart it so it lets go of the one it has.
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
    systemctl restart shop.service shop-worker.service
    ;;
  inodes)
    sed -i 's/^17 3 \* \* 0 shop /17 * * * * shop /' /etc/cron.d/shop-maintenance
    find /data/sessions -maxdepth 1 -name 'sess_*' -mmin +60 -delete
    ;;
esac
systemctl restart mysql.service
