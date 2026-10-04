#!/usr/bin/env bash
# Reference mitigation: stop the thumbnail service and free the disk. Redis
# accepts writes again once a save succeeds. Photos no longer get
# thumbnails.
set -euo pipefail
systemctl stop shop-thumbs.service
rm -rf /var/tmp/shop-thumbs
redis-cli bgsave >/dev/null
