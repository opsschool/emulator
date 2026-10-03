#!/usr/bin/env bash
# Reference fix: roll back to 2.3.0 and keep the pool below max_connections.
set -euo pipefail
ln -sfn /opt/shop/releases/2.3.0 /opt/shop/current
sed -i 's/^SHOP_DB_POOL_SIZE=.*/SHOP_DB_POOL_SIZE=20/' /etc/shop/shop.env
systemctl restart shop.service
