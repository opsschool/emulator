Why won't MySQL start? `systemctl status mysql`, then the end of `/var/log/mysql/error.log`.
---
MySQL's data is on /data, but which other paths does it write to? `mysql --help --verbose | grep -E '^(datadir|tmpdir)'` shows its settings. How full are those filesystems?
---
The root disk is full. What filled it, and does it come back after you clean up? `du -xh --max-depth=2 / | sort -h | tail`.
---
Who writes there? `journalctl -u shop-thumbs`, and `systemctl status shop-thumbs` for its restart count.
