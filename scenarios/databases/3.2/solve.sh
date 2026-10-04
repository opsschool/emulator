#!/usr/bin/env bash
# Reference fix: keep the hardening, with a limit that leaves room.
set -euo pipefail
sed -i 's/^TasksMax=.*/TasksMax=4096/' /etc/systemd/system/shop.service.d/60-hardening.conf
systemctl daemon-reload
systemctl restart shop.service
