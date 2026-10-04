#!/usr/bin/env bash
# MySQL is running, its AppArmor profile is loaded in enforce mode, and it
# can use its temporary directory.
set -euo pipefail
if ! systemctl is-active --quiet mysql.service; then
  echo "mysql is not running"
  exit 1
fi
if ! grep -q '^/usr/sbin/mysqld (enforce)$' /sys/kernel/security/apparmor/profiles; then
  echo "mysqld's AppArmor profile is not loaded in enforce mode"
  exit 1
fi
# A sort that spills to disk writes to tmpdir.
mysql -N -B -e "SET SESSION sort_buffer_size = 32768; SELECT id FROM shop.orders ORDER BY payment_ref LIMIT 1 OFFSET 200000" >/dev/null
