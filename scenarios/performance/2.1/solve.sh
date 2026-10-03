#!/usr/bin/env bash
# Reference fix: size concurrency to the memory available.
set -euo pipefail
env_file=/etc/shop/shop.env
sed -i -e '/^# Raised from 4 to clear the order backlog/d' \
  -e 's/^SHOP_WORKER_CONCURRENCY=.*/SHOP_WORKER_CONCURRENCY=4/' "$env_file"
systemctl restart shop-worker.service
