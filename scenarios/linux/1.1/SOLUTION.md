# Disk full

## What happened

Someone set `SHOP_LOG_LEVEL=debug` in `/etc/shop/shop.env` and pointed the
log at a new file to chase a bug, then forgot to turn it back. Debug logging
writes several lines per request. The log filled `/data`, which also holds
the MySQL data directory. MySQL could not write, so `POST /orders` failed
while reads, served mostly from Redis, kept working.

## Mitigate

Free space. Truncating the log works:

    truncate -s 0 /data/log/shop/debug.log

Deleting it with `rm` does not free the space while the shop still has the
file open; see `lsof +L1`. Restart MySQL if it does not recover on its own.

## Fix

- Set `SHOP_LOG_LEVEL=info` and restart `shop.service`.
- Add a logrotate rule for `/data/log/shop/*.log` with a size cap, so a
  noisy log can never fill the volume again.

## Curriculum

- [Filesystems 101](https://www.opsschool.org/filesystems_101.html)
- [Logs 101](https://www.opsschool.org/logs_101.html)
