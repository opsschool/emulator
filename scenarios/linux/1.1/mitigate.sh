#!/usr/bin/env bash
# Reference mitigation: free space. The cause (debug logging) is left in place.
set -euo pipefail
source /etc/shop/shop.env
truncate -s 0 "$SHOP_LOG_FILE"
systemctl restart mysql.service
