#!/usr/bin/env bash
# Reference mitigation: run the API on the previous config with a drop-in.
# shop.env is still wrong and the worker is still down.
set -euo pipefail
mkdir -p /etc/systemd/system/shop.service.d
cat >/etc/systemd/system/shop.service.d/override.conf <<'CONF'
[Service]
EnvironmentFile=
EnvironmentFile=/etc/shop/shop.env.bak
CONF
systemctl daemon-reload
systemctl reset-failed shop.service || true
systemctl restart shop.service
