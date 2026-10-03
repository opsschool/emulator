#!/usr/bin/env bash
set -euo pipefail
source /etc/shop/shop.env
[[ "${SHOP_LOG_LEVEL,,}" != debug ]]
