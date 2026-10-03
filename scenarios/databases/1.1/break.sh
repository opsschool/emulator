#!/usr/bin/env bash
# Release 2.4.0 leaks a database connection whenever an order names a
# product that no longer exists: the transaction is never rolled back. The
# release also raised the pool size past MySQL's max_connections, so the
# leak runs MySQL out of connections instead of just the app's pool.
set -euo pipefail

release=/opt/shop/releases/2.4.0
install -D -m 0755 /usr/local/lib/shop-builds/connleak/shop "$release/shop"
ln -sfn "$release" /opt/shop/current
sed -i 's/^SHOP_DB_POOL_SIZE=.*/SHOP_DB_POOL_SIZE=200/' /etc/shop/shop.env
printf '%s deploy shop 2.4.0 (was 2.3.0) by deploy-bot: ok\n' \
  "$(date -u +%Y-%m-%d)T$OPSSCHOOL_VAR_DEPLOYED_AT:00Z" >>/var/log/shop-deploy.log
systemctl restart shop.service

for _ in $(seq 60); do
  curl -fsS -o /dev/null http://127.0.0.1:8080/health && break
  sleep 1
done

# Fast-forward a few hours of orders for discontinued products, each of
# which leaks a connection, until MySQL refuses new ones.
max=$(mysql -N -B -e 'SELECT @@max_connections')
for _ in $(seq $((max + 20))); do
  curl -s -o /dev/null -m 5 -X POST -H 'Content-Type: application/json' \
    -d "{\"customer_id\":$((RANDOM + 1)),\"product_id\":$((900000 + RANDOM)),\"quantity\":1}" \
    http://127.0.0.1:8080/orders || true
done
