# Connection tracking table full

## What happened

Two changes went out in one maintenance window. The security team added a
stateful firewall rule (`-m conntrack --ctstate INVALID -j DROP`), which
makes the kernel track every connection and UDP flow. A tuning change in
`/etc/sysctl.d/60-kernel-tables.conf` set `net.netfilter.nf_conntrack_max`
to about 1000, because someone measured 640 tracked connections at 3 a.m.

Every TCP connection takes an entry, and keeps it for two minutes in
TIME_WAIT after it closes; every DNS lookup takes one too. At quiet times
the host stays under the limit. At peak the table fills, and the kernel
drops packets that would need a new entry: `nf_conntrack: table full,
dropping packet` in the kernel log. New connections from the load balancer
and lookups by the shop are dropped and retried until they time out, so a
share of requests hang for about 10 seconds or fail. The shop's own
metrics show nothing slow, because those requests never reach it. Only the
load balancer sees them: 504s in its access log (`upstream timed out while
connecting to upstream`) and in `edge_requests_total`.

## Mitigate

Raise the limit at runtime: `sysctl -w net.netfilter.nf_conntrack_max=262144`.

## Fix

Fix the tuning file so the limit survives a reboot, and keep the security
team's rule. `conntrack -S` and `/proc/sys/net/netfilter/nf_conntrack_count`
show how close the table runs to its limit; size it for peak, with room to
spare.

## Curriculum

- [Networking 201](https://www.opsschool.org/networking_201.html)
