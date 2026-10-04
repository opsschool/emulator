#!/usr/bin/env bash
# Learning the payments host's address from scratch, several times, always
# finds the right one, and nothing pins a different one.
set -euo pipefail
want=52:54:00:36:00:14
for _ in 1 2 3 4 5; do
  if ip neigh show 10.54.0.20 dev br-svc | grep -q PERMANENT; then
    have=$(ip neigh show 10.54.0.20 dev br-svc | awk '{print $3}')
    if [[ $have != "$want" ]]; then
      echo "a permanent entry maps 10.54.0.20 to $have"
      exit 1
    fi
  else
    ip neigh flush to 10.54.0.20 dev br-svc
  fi
  curl -fsS -m 3 http://10.54.0.20:8081/health >/dev/null || { echo "payments did not answer"; exit 1; }
  have=$(ip neigh show 10.54.0.20 dev br-svc | awk '{print $3}')
  if [[ $have != "$want" ]]; then
    echo "10.54.0.20 resolved to $have"
    exit 1
  fi
  sleep 5
done
if [[ -e /run/netns/pay-old ]] && ip -n pay-old addr show 2>/dev/null | grep -q '10.54.0.20/'; then
  echo "another host on the segment still holds 10.54.0.20"
  exit 1
fi
