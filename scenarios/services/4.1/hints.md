What does the shop say when checkout fails? `journalctl -u shop` around a failed order.
---
Checkout depends on Redis. Ask Redis what it thinks: `redis-cli info persistence`, and its log in `/var/log/redis`.
---
The disk is full. What filled it, and when did it start? `du -xsh /var/lib/* | sort -h`.
---
Those are crash dumps. `coredumpctl list` shows which program keeps crashing; `journalctl -u shop-thumbs` shows what it was doing.
---
Why were the dumps allowed to use the whole disk? `systemd-analyze cat-config systemd/coredump.conf`.
