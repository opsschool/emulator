#!/usr/bin/env bash
# Smoke test for the single-node image, run inside the VM: the shop serves
# through nginx and every exporter responds.
set -euo pipefail

fail=0
check() { # name, command...
  local name=$1 out
  shift
  if out=$("$@" 2>&1); then
    echo "ok   $name"
  else
    echo "FAIL $name"
    tail -n 5 <<<"$out" | sed 's/^/       /'
    fail=1
  fi
}

for unit in mysql redis-server nginx shop shop-worker shop-payments node_exporter process-exporter mysqld_exporter redis_exporter alloy; do
  check "unit $unit active" systemctl is-active --quiet "$unit"
done
check "health through nginx" curl -fsS http://127.0.0.1/health
check "catalog through nginx" curl -fsS http://127.0.0.1/products
check "order through nginx" curl -sS --fail-with-body -X POST -d '{"customer_id":1,"product_id":1,"quantity":1}' http://127.0.0.1/orders
check "shop metrics" curl -fsS http://127.0.0.1:9091/metrics
check "worker metrics" curl -fsS http://127.0.0.1:9092/metrics
check "node_exporter" curl -fsS http://127.0.0.1:9100/metrics
check "process-exporter" curl -fsS http://127.0.0.1:9256/metrics
check "mysqld_exporter up" bash -c 'curl -fsS http://127.0.0.1:9104/metrics | grep -q "^mysql_up 1"'
check "redis_exporter up" bash -c 'curl -fsS http://127.0.0.1:9121/metrics | grep -q "^redis_up 1"'
check "/data mounted" mountpoint -q /data
check "payments resolves" getent hosts payments.shop.internal
check "https api, full chain" curl -fsS --resolve api.shop.internal:443:127.0.0.1 https://api.shop.internal/health
check "https partners, full chain" curl -fsS --resolve partners.shop.internal:443:127.0.0.1 https://partners.shop.internal/health
check "persisted firewall" test -s /etc/iptables/rules.v4
if [[ $(systemd-detect-virt --container || true) == none ]]; then
  check "swap on" bash -c '[[ -n $(swapon --noheadings) ]]'
  check "unit dnsmasq active" systemctl is-active --quiet dnsmasq
  # Since dnsmasq last started: provisioning starts it once with stock defaults.
  dns_warnings=$(journalctl -q -p warning -t dnsmasq -t resolvconf --since "$(systemctl show -P InactiveExitTimestamp dnsmasq)")
  check "no dns warnings from dnsmasq" test -z "$dns_warnings"
  [[ -z $dns_warnings ]] || sed 's/^/       /' <<<"$dns_warnings"
fi
# shellcheck disable=SC2016 # expanded by the inner bash
check "orders seeded" bash -c '(( $(mysql -N -B shop -e "SELECT COUNT(*) FROM orders") > 1000 ))'
exit $fail
