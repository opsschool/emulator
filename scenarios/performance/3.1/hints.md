The dashboards show plenty of idle CPU. Is the shop itself getting the CPU it asks for? Compare the shop's CPU use in `top` with how slow it is.
---
Look at how systemd runs the shop: `systemctl status shop` and `systemctl cat shop`. What changed recently?
---
Units can be grouped into slices, and a slice can have limits. `systemd-cgls` and `systemd-cgtop` show the tree.
---
The kernel counts every time it holds a group back: look at `cpu.stat` (`nr_throttled`, `throttled_usec`) in the shop's cgroup and its parents under `/sys/fs/cgroup`.
