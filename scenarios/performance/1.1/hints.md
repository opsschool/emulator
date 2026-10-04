The slowness comes and goes on a pattern. When it is slow, which processes use the CPU, and who started them? `ps -eo pid,ppid,ni,pcpu,etime,args --sort=-pcpu | head`, then follow the parent IDs.
