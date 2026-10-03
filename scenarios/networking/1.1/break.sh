#!/usr/bin/env bash
# A network config push points the machine's site resolver at an address
# that no longer answers. Lookups of internal names, such as the payments
# service, time out.
set -euo pipefail

conf=/etc/systemd/resolved.conf.d/site-dns.conf
sed -i "s/^DNS=.*/DNS=$OPSSCHOOL_VAR_DEAD_RESOLVER/" "$conf"
systemctl restart systemd-resolved.service
logger -t netconfig "applied revision 2291 (NET-412): site DNS -> $OPSSCHOOL_VAR_DEAD_RESOLVER"
# The push restarts services that hold network connections.
systemctl restart shop.service shop-worker.service
logger -t netconfig "restarted shop.service shop-worker.service"
