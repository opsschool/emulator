#!/usr/bin/env bash
# Reference fix: roll the thumbnail service back, clear the dumps, put the
# core dump limits back, and let Redis save again.
set -euo pipefail
ln -sfn /opt/shop-thumbs/releases/1.4.2 /opt/shop-thumbs/current
systemctl restart shop-thumbs.service
sed -i '/^MaxUse=/d; /^KeepFree=/d' /etc/systemd/coredump.conf.d/50-thumbs-vendor.conf
rm -f /var/lib/systemd/coredump/*
redis-cli config set stop-writes-on-bgsave-error yes >/dev/null
redis-cli bgsave >/dev/null
