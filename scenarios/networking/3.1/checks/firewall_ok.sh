#!/usr/bin/env bash
# The persisted firewall keeps the security team's rule.
set -euo pipefail
if ! grep -q -- '--ctstate INVALID -j DROP' /etc/iptables/rules.v4; then
  echo "the persisted firewall no longer drops invalid packets"
  exit 1
fi
