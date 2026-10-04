#!/usr/bin/env bash
# Reference fix: size the table in the tuning file, and apply it.
set -euo pipefail
sed -i 's/^net.netfilter.nf_conntrack_max = .*/net.netfilter.nf_conntrack_max = 262144/' /etc/sysctl.d/60-kernel-tables.conf
sysctl -q -p /etc/sysctl.d/60-kernel-tables.conf
