#!/usr/bin/env bash
# Release 2.4.0's worker leaves a transaction open on orders every time its
# reconcile loop finds nothing stuck, once a minute. Each idle transaction
# holds a shared metadata lock on orders. The gift-note migration's ALTER
# TABLE needs an exclusive lock, so it waits, and every query on orders
# after it queues behind the waiting ALTER.
set -euo pipefail

# Not part of the fault. The queries that pile up behind the ALTER can fill
# all 151 of MySQL's connections, and then nobody can get in to look. Cap
# the shop's account below that, as shared database servers usually do.
mysql -e "ALTER USER 'shop'@'localhost' WITH MAX_USER_CONNECTIONS 120"

release=/opt/shop/releases/2.4.0
install -D -m 0755 /usr/local/lib/shop-builds/idletx/shop "$release/shop"
ln -sfn "$release" /opt/shop/current
printf '%s deploy shop 2.4.0 (was 2.3.0) by deploy-bot: ok\n' "$(date -u +%FT%TZ)" >>/var/log/shop-deploy.log
systemctl restart shop-worker.service shop.service

for _ in $(seq 60); do
  curl -fsS -o /dev/null http://127.0.0.1:8080/health && break
  sleep 1
done
# The worker reconciles as it starts and then once a minute; wait for its
# transaction: idle for a few seconds, so not one of the shop's own
# between statements. Allow a few minutes in case the first try fails.
# Without it the migration would apply at once and nothing would hang.
idle=0
for _ in $(seq 150); do
  idle=$(mysql -N -B -e "SELECT COUNT(*) FROM information_schema.innodb_trx t
    JOIN information_schema.processlist p ON p.id = t.trx_mysql_thread_id
    WHERE p.user = 'shop' AND p.command = 'Sleep'
      AND t.trx_started < NOW() - INTERVAL 3 SECOND")
  ((idle > 0)) && break
  sleep 1
done
if ((idle == 0)); then
  echo "the worker never left a transaction open" >&2
  exit 1
fi

name="${OPSSCHOOL_VAR_MIGRATION}_orders_add_gift_note"
dir=/opt/shop/migrations
mkdir -p "$dir"
cat >"$dir/$name.sql" <<'SQL'
-- Gift notes for the holiday range. ADD COLUMN at the end is instant in
-- MySQL 8: it only changes the table's metadata.
ALTER TABLE orders ADD COLUMN gift_note VARCHAR(255) NULL;
SQL
printf '%s applying %s\n' "$(date -u +%FT%TZ)" "$name" >>/var/log/shop-migrate.log
systemd-run --quiet --unit=shop-migrate --description="Shop schema migration $name" --collect \
  bash -c "mysql shop <'$dir/$name.sql' && printf '%s applied %s\n' \"\$(date -u +%FT%TZ)\" '$name' >>/var/log/shop-migrate.log"
