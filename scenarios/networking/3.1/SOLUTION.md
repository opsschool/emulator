# Connection tracking table full

## What happened

Two changes went out in one maintenance window. The security team added a
stateful firewall rule (`-m conntrack --ctstate INVALID -j DROP`), which
makes the kernel track every connection and UDP flow. A tuning change in
`/etc/sysctl.d/60-kernel-tables.conf` set `net.netfilter.nf_conntrack_max`
to a few hundred entries, on the theory that the host handles few
connections.

Every DNS lookup, every new TCP connection, and every connection in
TIME_WAIT takes an entry. The shop resolves the payments service by name
for every order, so checkout creates a stream of short-lived entries. At
peak the table fills, and the kernel drops packets that would need a new
entry: `nf_conntrack: table full, dropping packet` in the kernel log. DNS
lookups for payments time out, and checkout fails. Browsing reuses
long-lived connections and keeps working.

## Mitigate

Raise the limit at runtime: `sysctl -w net.netfilter.nf_conntrack_max=262144`.

## Fix

Fix the tuning file so the limit survives a reboot, and keep the security
team's rule. `conntrack -S` and `/proc/sys/net/netfilter/nf_conntrack_count`
show how close the table runs to its limit.

## Curriculum

- [Networking 201](https://www.opsschool.org/networking_201.html)
