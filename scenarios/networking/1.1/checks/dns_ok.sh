#!/usr/bin/env bash
# Passes when payments.shop.internal resolves through the system's DNS
# servers. /etc/hosts entries do not count: they hide a broken resolver.
set -euo pipefail
name=payments.shop.internal
if grep -qE '^nameserver 127\.0\.0\.53$' /etc/resolv.conf; then
  timeout 10 resolvectl query --synthesize=no --cache=no "$name" >/dev/null ||
    { echo "systemd-resolved cannot resolve $name"; exit 1; }
  exit 0
fi
# A static resolv.conf: every listed nameserver must answer.
servers=$(awk '$1 == "nameserver" {print $2}' /etc/resolv.conf)
[[ -n "$servers" ]] || { echo "no nameservers in /etc/resolv.conf"; exit 1; }
for s in $servers; do
  [[ -n $(dig +short +time=2 +tries=1 "@$s" "$name") ]] || { echo "$s does not answer for $name"; exit 1; }
done
