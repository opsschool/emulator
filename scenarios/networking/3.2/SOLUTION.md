# One neighbor unreachable

## What happened

The payments host sits on the service segment, the `br-svc` bridge, at
10.54.0.20 with MAC `52:54:00:36:00:14`. To send it a packet, the shop's
machine needs that MAC, which it learns with ARP and keeps in its neighbor
table (`ip neigh`). Something gave it the wrong one:

- **pinned:** during a NIC replacement the network team pinned the payments
  host's MAC as a static `[Neighbor]` in
  `/etc/systemd/network/br-svc.network`, with two digits swapped (`:41`
  instead of `:14`). A permanent entry is never re-learned, so every packet
  goes to a MAC nobody has. The payments host never sees them.
- **duplicate:** the previous payments host (`payments-old.service`, a
  namespace called `pay-old`) was kept running on the same segment with the
  same IP. Its HA agent announces the address every 20 seconds, and each
  announcement overwrites the entry, so it points at the old host nearly
  all the time. Connections to it are refused, and almost every checkout
  fails.

Everything else works: DNS resolves, the payments host can reach the shop,
and other neighbors are fine. `ip neigh show 10.54.0.20` against
`ip -n pay1 link show eth0` shows the mismatch, and `tcpdump -eni br-svc
arp` shows who is answering.

## Mitigate

Pin the right MAC at runtime:
`ip neigh replace 10.54.0.20 lladdr 52:54:00:36:00:14 nud permanent dev br-svc`.
Or, for the duplicate, take the old host's link down.

## Fix

Remove the source of the wrong answer so it doesn't come back after a
reboot: delete the stale `[Neighbor]` section and reload networkd, or retire
the old host (`systemctl disable --now payments-old`). Then let ARP learn
the address again (`ip neigh del 10.54.0.20 dev br-svc`). Don't leave a
static pin in place of the real fix: the next NIC swap breaks it again.

## Curriculum

- [Networking 101: ARP](https://www.opsschool.org/networking_101.html#arp)
