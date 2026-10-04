Which requests fail, and when? Compare quiet times with peaks. Failed requests take about 10 seconds: what gives up after that long?
---
The shop's own latency metrics look fine, so the time is lost before requests reach it. Is the kernel dropping packets? Look at the kernel log: `journalctl -k` or `dmesg`.
---
`conntrack -S` and the files under `/proc/sys/net/netfilter/` show how many connections the kernel tracks and its limit. Who set the limit?
