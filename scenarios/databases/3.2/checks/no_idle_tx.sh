#!/usr/bin/env bash
# Passes when no shop connection has sat idle inside a transaction for more
# than two minutes. Verify replays load for longer than that after
# restarting the worker, which reconciles as it starts.
set -euo pipefail
n=$(mysql -N -B -e "SELECT COUNT(*) FROM information_schema.innodb_trx t
  JOIN information_schema.processlist p ON p.id = t.trx_mysql_thread_id
  WHERE p.user = 'shop' AND p.command = 'Sleep' AND t.trx_started < NOW() - INTERVAL 2 MINUTE")
echo "idle transactions older than 2m: $n"
((n == 0))
