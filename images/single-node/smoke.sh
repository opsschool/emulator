#!/usr/bin/env bash
# Smoke test for the single-node image, run inside the VM: the shop serves
# through nginx and every exporter responds.
set -euo pipefail

fail=0
check() { # name, command...
  local name=$1
  shift
  if "$@" >/dev/null 2>&1; then
    echo "ok   $name"
  else
    echo "FAIL $name"
    fail=1
  fi
}

for unit in mysql redis-server nginx shop shop-worker shop-payments node_exporter process-exporter mysqld_exporter redis_exporter alloy; do
  check "unit $unit active" systemctl is-active --quiet "$unit"
done
check "health through nginx" curl -fsS http://127.0.0.1/health
check "catalog through nginx" curl -fsS http://127.0.0.1/products
check "order through nginx" curl -fsS -X POST -d '{"customer_id":1,"product_id":1,"quantity":1}' http://127.0.0.1/orders
check "shop metrics" curl -fsS http://127.0.0.1:9091/metrics
check "worker metrics" curl -fsS http://127.0.0.1:9092/metrics
check "node_exporter" curl -fsS http://127.0.0.1:9100/metrics
check "process-exporter" curl -fsS http://127.0.0.1:9256/metrics
check "mysqld_exporter up" bash -c 'curl -fsS http://127.0.0.1:9104/metrics | grep -q "^mysql_up 1"'
check "redis_exporter up" bash -c 'curl -fsS http://127.0.0.1:9121/metrics | grep -q "^redis_up 1"'
check "/data mounted" mountpoint -q /data
# shellcheck disable=SC2016 # expanded by the inner bash
check "orders seeded" bash -c '(( $(mysql -N -B shop -e "SELECT COUNT(*) FROM orders") > 1000 ))'
exit $fail
