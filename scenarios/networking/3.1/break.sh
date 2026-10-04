#!/usr/bin/env bash
# Two changes from one maintenance window. The firewall now tracks
# connection state, which loads the kernel's connection tracking. A tuning
# change caps the tracking table at a few hundred entries. Every DNS lookup
# and new connection takes an entry, so at peak the table fills and the
# kernel drops new packets. The shop looks up payments for every order.
set -euo pipefail

rules=/etc/iptables/rules.v4
sed -i 's/^# MySQL and Redis are local-only.$/# Drop packets that belong to no known connection (SEC-140).\n-A INPUT -m conntrack --ctstate INVALID -j DROP\n&/' "$rules"
iptables-restore <"$rules"
cat >/etc/sysctl.d/60-kernel-tables.conf <<CONF
# PERF-77: trim kernel tables to what this host uses. It handles a few
# dozen connections at a time.
net.netfilter.nf_conntrack_max = $OPSSCHOOL_VAR_MAX
CONF
sysctl -q -p /etc/sysctl.d/60-kernel-tables.conf
logger -t maint "SEC-140 stateful firewall, PERF-77 kernel tables applied"
