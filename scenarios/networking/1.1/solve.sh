#!/usr/bin/env bash
# Reference fix: point the persisted resolver config back at the working
# site resolver and drop the hosts-file pin.
set -euo pipefail
sed -i 's/^DNS=.*/DNS=10.53.0.10/' /etc/systemd/resolved.conf.d/site-dns.conf
sed -i '/payments\.shop\.internal/d' /etc/hosts
systemctl restart systemd-resolved.service shop.service
