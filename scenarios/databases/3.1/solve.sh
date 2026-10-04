#!/usr/bin/env bash
# Reference fix: allow the new directory in the profile's local additions,
# reload the profile and start MySQL.
set -euo pipefail
dir="$OPSSCHOOL_VAR_TMPDIR"
install -d /etc/apparmor.d/local
printf '%s/ rw,\n%s/** rwk,\n' "$dir" "$dir" >>/etc/apparmor.d/local/usr.sbin.mysqld
apparmor_parser -r /etc/apparmor.d/usr.sbin.mysqld
systemctl restart mysql.service
systemctl restart shop.service shop-worker.service
