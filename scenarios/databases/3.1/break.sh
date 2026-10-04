#!/usr/bin/env bash
# A tuning change moves MySQL's temporary files to the data volume. The
# directory's ownership and mode are right, but Ubuntu's AppArmor profile
# for mysqld only allows the paths it knows, so mysqld is denied and fails
# to start.
set -euo pipefail

dir="$OPSSCHOOL_VAR_TMPDIR"
install -d -o mysql -g mysql -m 0750 "$dir"
cat >/etc/mysql/mysql.conf.d/zz-tuning.cnf <<CNF
# DB-212: keep MySQL's temporary files off the root disk. Large sorts filled
# it during last quarter's reporting run.
[mysqld]
tmpdir = $dir
CNF
logger -t db-change "DB-212: tmpdir -> $dir, restarting mysql"
systemctl restart mysql.service || true
