#!/usr/bin/env bash
# Worker concurrency is raised to clear an order backlog. Each worker keeps a
# 256 MB render buffer, so the worker now wants more memory than the machine
# has, and the kernel swaps everything, the shop and MySQL included.
set -euo pipefail
env_file=/etc/shop/shop.env
sed -i "s/^SHOP_WORKER_CONCURRENCY=.*/# Raised from 4 to clear the order backlog (OPS-2210).\nSHOP_WORKER_CONCURRENCY=$OPSSCHOOL_VAR_CONCURRENCY/" "$env_file"
systemctl restart shop-worker.service
