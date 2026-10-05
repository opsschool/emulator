#!/usr/bin/env bash
# Put the shop's payment client back to a 2s timeout and two retries with
# backoff, so a
# slow gateway sees at most three calls per order inside the 5s request
# deadline. Keep the gateway's limit, and start it fresh to drop the queue.
set -euo pipefail
rm -f /etc/systemd/system/shop-payments.service.d/zz-incident.conf
env_file=/etc/shop/shop.env
sed -i -e '/^# PAY-311/,/^# the gateway is slow\.$/d' \
  -e 's/^SHOP_CLIENT_TIMEOUT=.*/SHOP_CLIENT_TIMEOUT=2s/' \
  -e 's/^SHOP_RETRY_MAX=.*/SHOP_RETRY_MAX=2/' \
  -e 's/^SHOP_RETRY_BACKOFF=.*/SHOP_RETRY_BACKOFF=100ms/' "$env_file"
systemctl daemon-reload
systemctl restart shop-payments.service shop.service
