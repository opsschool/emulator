#!/usr/bin/env bash
# Reference mitigation: unload mysqld's AppArmor profile and start MySQL.
# The profile is loaded again at boot.
set -euo pipefail
apparmor_parser -R /etc/apparmor.d/usr.sbin.mysqld
systemctl restart mysql.service
systemctl restart shop.service shop-worker.service
