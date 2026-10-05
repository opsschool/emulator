#!/usr/bin/env bash
# With the payments team's OK, lift the gateway's limit for now and drop
# the queued work. Checkout recovers, but the shop's retries are still
# aggressive, and the limit has to come back.
set -euo pipefail
cat >/etc/systemd/system/shop-payments.service.d/zz-incident.conf <<'UNIT'
[Service]
ExecStart=
ExecStart=/opt/shop/current/shop payments --listen 127.0.0.1:8081 --workers 16 --min-latency 100ms --max-latency 180ms
UNIT
systemctl daemon-reload
systemctl restart shop-payments.service
