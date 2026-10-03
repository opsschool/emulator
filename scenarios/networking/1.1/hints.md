The payments service hasn't seen any requests, so the shop's calls aren't reaching it. What error does the shop log for failed orders? `journalctl -u shop -p err -n 20`.
---
The shop reaches payments by name. Can this machine resolve it? Try `dig payments.shop.internal` and `resolvectl status`.
---
Which DNS server is the machine asking, and does anything answer there? `ss -lunp` shows what listens on port 53 here. `journalctl -t netconfig` shows recent network changes.
---
An `/etc/hosts` entry gets checkout working but leaves DNS broken. Fix the resolver config that systemd-resolved reads, in `/etc/systemd/resolved.conf.d/`, so it survives a reboot.
