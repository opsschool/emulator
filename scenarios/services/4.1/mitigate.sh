#!/usr/bin/env bash
# Reference mitigation: stop the thumbnail service, free the disk, and bring
# MySQL back. Photos no longer get thumbnails.
set -euo pipefail
systemctl stop shop-thumbs.service
rm -rf /var/tmp/shop-thumbs
systemctl reset-failed mysql.service
systemctl start mysql.service
redis-cli bgsave >/dev/null
