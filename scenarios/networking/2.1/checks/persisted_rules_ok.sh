#!/usr/bin/env bash
# Loads the persisted IPv4 rules into a scratch network namespace and
# connects to the app port over loopback there, as nginx would after a
# reboot. The live firewall is not touched.
set -euo pipefail
ns=opsschool-fwcheck
rules=/etc/iptables/rules.v4
ip netns del "$ns" 2>/dev/null || true
ip netns add "$ns"
trap 'kill "$listener" 2>/dev/null || true; ip netns del "$ns" 2>/dev/null || true' EXIT
ip -n "$ns" link set lo up
if [[ -f "$rules" ]]; then
  ip netns exec "$ns" iptables-restore <"$rules"
fi
ip netns exec "$ns" ncat -l -k 127.0.0.1 8080 >/dev/null 2>&1 &
listener=$!
sleep 0.5
if ! ip netns exec "$ns" timeout 3 bash -c 'exec 3<>/dev/tcp/127.0.0.1/8080'; then
  echo "with the persisted rules, connections to 127.0.0.1:8080 are blocked"
  exit 1
fi
