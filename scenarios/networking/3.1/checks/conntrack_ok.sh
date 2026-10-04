#!/usr/bin/env bash
# The configured limit, from sysctl files, and the live limit are both at
# least 65536.
set -euo pipefail
live=$(sysctl -n net.netfilter.nf_conntrack_max)
if ((live < 65536)); then
  echo "nf_conntrack_max is $live"
  exit 1
fi
conf=$({ grep -hs '^ *net.netfilter.nf_conntrack_max' /etc/sysctl.conf /etc/sysctl.d/*.conf /run/sysctl.d/*.conf /usr/lib/sysctl.d/*.conf || true; } | tail -1 | cut -d= -f2 | tr -d ' ')
if [[ -n $conf ]] && ((conf < 65536)); then
  echo "a sysctl file sets nf_conntrack_max to $conf"
  exit 1
fi
