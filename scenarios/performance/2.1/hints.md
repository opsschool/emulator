CPU is low but everything is slow. What else can make a machine slow? Look at memory on the dashboard, or `free -m` and `vmstat 1`.
---
In `vmstat 1`, watch the `si` and `so` columns: pages swapped in and out per second. Anything above zero, all the time, means the machine needs more memory than it has.
---
Which process wants the memory? `ps -eo pid,rss,vsz,args --sort=-rss | head`, or `top` sorted by memory (press M).
---
The worker's memory depends on its settings in `/etc/shop/shop.env`. What changed, and how much memory does each worker need?
