#!/usr/bin/env bash
# While /data was unmounted for maintenance, the shop was started again and
# wrote its logs into the empty /data directory on the root filesystem:
# nothing makes shop.service wait for /data. When /data was mounted again it
# covered those files. They still use the root disk, but `du` can't see them.
set -euo pipefail

# Everything that holds files open on /data stops for the maintenance.
# Alloy tails the shop log on /data; it is started again below.
systemctl stop alloy.service shop.service shop-worker.service mysql.service # lint:allow alloy
umount /data
logger -t data-maint "INFRA-88: /data offline for integrity check"

# The shop comes back while the volume is still offline. Its log directory
# is left over from an earlier maintenance window.
install -d -o shop -g shop /data/log/shop /data/sessions
systemctl start shop.service
sleep 5
systemctl stop shop.service
# Hours of that, enough to fill the root disk.
f=/data/log/shop/app.log
size=$(df --output=size -B1 / | tail -1)
keep=$((size * 3 / 200))
avail() { df --output=avail -B1 / | tail -1; }
while (($(avail) > keep + (1 << 30))); do
  fallocate -o "$(stat -c %s "$f")" -l 1G "$f" || break
done
left=$(($(avail) - keep))
((left > 0)) && { fallocate -o "$(stat -c %s "$f")" -l "$left" "$f" || true; }

mount /data
systemctl start mysql.service shop-worker.service shop.service alloy.service # lint:allow alloy
logger -t data-maint "INFRA-88: /data back online"
