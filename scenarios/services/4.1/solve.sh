#!/usr/bin/env bash
# Reference fix: roll the thumbnail service back, so it rejects the bad
# photo, free the disk, and bring MySQL back.
set -euo pipefail
ln -sfn /opt/shop-thumbs/releases/1.4.2 /opt/shop-thumbs/current
systemctl stop shop-thumbs.service
rm -rf /var/tmp/shop-thumbs
systemctl start shop-thumbs.service
systemctl reset-failed mysql.service
systemctl start mysql.service
redis-cli config set stop-writes-on-bgsave-error yes >/dev/null
redis-cli bgsave >/dev/null
