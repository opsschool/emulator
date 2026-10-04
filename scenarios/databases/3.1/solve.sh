#!/usr/bin/env bash
# Reference fix: allow the new directory in the profile's local additions,
# reload the profile and start MySQL.
set -euo pipefail
dir="$OPSSCHOOL_VAR_TMPDIR"
profile=$(grep -l '/usr/sbin/mysqld' /etc/apparmor.d/* 2>/dev/null | head -1)
install -d /etc/apparmor.d/local
printf '%s/ rw,\n%s/** rwk,\n' "$dir" "$dir" >>"/etc/apparmor.d/local/$(basename "$profile")"
apparmor_parser -r "$profile"
systemctl restart mysql.service
systemctl restart shop.service shop-worker.service
