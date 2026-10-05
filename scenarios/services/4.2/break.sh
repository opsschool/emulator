#!/usr/bin/env bash
# Two changes, each harmless alone:
#  - The payments team caps the gateway at two authorizations at a time,
#    at production latency (100-180ms): about 14 a second, plenty for the
#    shop's 10 orders a second at peak.
#  - The shop's payment client now times out after 500ms instead of 2s and
#    retries 6 to 10 times instead of 2, at once, with no budget.
# At the next traffic peak a few authorizations wait longer than 500ms.
# Each timeout becomes a retry, and the gateway finishes every request it
# accepted, including the ones nobody is waiting for. Retries outgrow its
# capacity, and once the line is longer than 500ms every attempt times out:
# seven or more calls per order, inside the shop's 5s request deadline,
# more than the gateway can do even at normal traffic. The overload sustains itself after the peak ends.
set -euo pipefail

dropin=/etc/systemd/system/shop-payments.service.d/concurrency.conf
mkdir -p "$(dirname "$dropin")"
cat >"$dropin" <<'UNIT'
# PAY-298: the card network allows two authorizations in flight per
# merchant. Match it here so we find out before they reject us.
[Service]
ExecStart=
ExecStart=/opt/shop/current/shop payments --listen 127.0.0.1:8081 --workers 2 --min-latency 100ms --max-latency 180ms
UNIT
systemctl daemon-reload
systemctl restart shop-payments.service
logger -t payments-deploy "PAY-298 gateway concurrency limit applied"

env_file=/etc/shop/shop.env
sed -i \
  -e "s/^SHOP_CLIENT_TIMEOUT=.*/# PAY-311: fail fast and retry rather than leave checkout hanging when\n# the gateway is slow.\nSHOP_CLIENT_TIMEOUT=500ms/" \
  -e "s/^SHOP_RETRY_MAX=.*/SHOP_RETRY_MAX=$OPSSCHOOL_VAR_RETRY_MAX/" \
  -e "s/^SHOP_RETRY_BACKOFF=.*/SHOP_RETRY_BACKOFF=0s/" \
  "$env_file"
systemctl restart shop.service
logger -t confd "PAY-311 applied to /etc/shop/shop.env; restarted shop.service"

# Wait for a traffic peak to start the overload (they come every three
# minutes) and for the gateway's line to grow.
log=/data/log/shop/app.log
for _ in $(seq 60); do
  n=$(tail -n 200 "$log" | grep -o '"msg":"authorizations waiting for a worker".*"waiting":[0-9]*' | tail -1 | grep -o '[0-9]*$' || true)
  ((${n:-0} > 300)) && break
  sleep 5
done
