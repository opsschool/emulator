Which requests fail, and when? Compare checkout with browsing, and quiet times with peaks.
---
Checkout calls the payments service by name. What do the shop's errors say? `journalctl -u shop`.
---
The kernel may be dropping packets. Look at the kernel log: `journalctl -k` or `dmesg`.
---
`conntrack -S`, or the files under `/proc/sys/net/netfilter/`, show how full the connection tracking table is. Who set its size?
