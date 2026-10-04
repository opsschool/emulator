#!/usr/bin/env bash
# Reference fix: roll the thumbnail service back, so it rejects the bad
# photo, free the disk, and let Redis save.
set -euo pipefail
ln -sfn /opt/shop-thumbs/releases/1.4.2 /opt/shop-thumbs/current
systemctl stop shop-thumbs.service
rm -rf /var/tmp/shop-thumbs
systemctl start shop-thumbs.service
redis-cli bgsave >/dev/null
