Why won't MySQL start? `systemctl status mysql` and its error log, `/var/log/mysql/error.log`.
---
MySQL says it can't write to its new directory, but the permissions look right. What else can stop a process from opening a file?
---
Look in the kernel log around the time MySQL started: `journalctl -k` or `dmesg`. Search for `DENIED`.
---
AppArmor confines mysqld to the paths in its profile. `aa-status` lists confined processes; profiles live in `/etc/apparmor.d`, and local additions in `/etc/apparmor.d/local`.
