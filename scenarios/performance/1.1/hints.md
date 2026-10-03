Look at the latency graph closely. Is it slow all the time, or in a pattern?
---
When it is slow, what is the CPU doing? `top`, or `mpstat 1` to see it over time.
---
Which processes are using the CPU, and who started them? `ps -eo pid,ppid,ni,pcpu,etime,args --sort=-pcpu | head`. Note the NI column, then follow the parent IDs up with `ps -fp <ppid>`.
---
Something starts every minute. Look in `/etc/cron.d`.
