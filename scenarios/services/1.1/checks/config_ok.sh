#!/usr/bin/env bash
# Passes when the shop accepts /etc/shop/shop.env.
set -euo pipefail
env -i bash -c 'set -a; source /etc/shop/shop.env; set +a; exec /opt/shop/current/shop check-config'
