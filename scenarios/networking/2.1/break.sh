#!/usr/bin/env bash
# A hardening change restricts the app port to the proxy, but the rule has
# no exception for loopback, which is how the proxy reaches the app. It is
# applied live and saved to the persisted firewall config.
set -euo pipefail

rules=/etc/iptables/rules.v4
rule=(-p tcp --dport 8080 -m comment --comment "app port: proxy only (SEC-2291)" -j "$OPSSCHOOL_VAR_ACTION")
iptables -A INPUT "${rule[@]}"
# Save it the way the change was rolled out: into the persisted rules.
sed -i "/^COMMIT/i -A INPUT -p tcp -m tcp --dport 8080 -m comment --comment \"app port: proxy only (SEC-2291)\" -j $OPSSCHOOL_VAR_ACTION" "$rules"
