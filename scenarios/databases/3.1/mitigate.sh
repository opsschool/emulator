#!/usr/bin/env bash
# Reference mitigation: unload mysqld's AppArmor profile and start MySQL.
# The profile is loaded again at boot.
set -euo pipefail
apparmor_parser -R "$(grep -l '/usr/sbin/mysqld' /etc/apparmor.d/* 2>/dev/null | head -1)"
systemctl restart mysql.service
systemctl restart shop.service shop-worker.service
