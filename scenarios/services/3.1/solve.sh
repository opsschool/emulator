#!/usr/bin/env bash
# Reference fix: correct the repository, and keep the sync running.
set -euo pipefail
tmp=$(mktemp -d)
git clone -q /srv/git/shop-config.git "$tmp"
sed -i 's|^SHOP_PAYMENTS_URL=.*|SHOP_PAYMENTS_URL=http://payments.shop.internal:8081|' "$tmp/shop.env"
git -C "$tmp" -c user.name=oncall -c user.email=oncall@shop.internal \
  commit -q -am "Revert payments endpoint: the new gateway does not exist yet"
git -C "$tmp" push -q origin main
rm -rf "$tmp"
systemctl enable --now config-sync.timer
systemctl start config-sync.service
