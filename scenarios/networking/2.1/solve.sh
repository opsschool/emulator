#!/usr/bin/env bash
# Reference fix: keep the intent (nobody but the proxy reaches the app port)
# but exempt loopback, in the persisted rules, then reload them.
set -euo pipefail
rules=/etc/iptables/rules.v4
sed -i 's/^-A INPUT -p tcp -m tcp --dport 8080 -m comment/-A INPUT ! -i lo -p tcp -m tcp --dport 8080 -m comment/' "$rules"
iptables-restore <"$rules"
