Why does the shop restart? `journalctl -u shop` around a restart: read the last lines before it dies.
---
The Go runtime couldn't create an OS thread. What limits how many threads a process can have? Look beyond memory.
---
`systemctl status shop` shows a `Tasks:` line with a limit. Where does that limit come from? `systemctl cat shop`.
---
Count the shop's threads at peak: `ps -o nlwp= -p $(pgrep -x shop)`, or `pids.current` in its cgroup.
