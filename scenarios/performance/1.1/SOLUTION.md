# Slow every minute

## What happened

A new cron job in `/etc/cron.d` builds a compressed product feed every
minute. It starts two `gzip -9` processes per CPU core, reading from
`/dev/urandom`, and runs for most of the minute. Someone gave it a higher
priority than the shop (`nice -n -10`) so the feed would never be late, so
the kernel hands it the CPU first. Latency rises for most of every minute
and recovers briefly in between. The saw-tooth pattern on the latency graph,
and on the CPU graph, is the giveaway.

`top` shows several `gzip` processes with a negative `NI`. Their parent is a shell script that
cron started, which `ps -ef --forest` or following `PPID` reveals.

## Mitigate

Kill the job, or stop cron. Killing it lasts until the next minute.
Stopping cron works, but it also stops every other scheduled job, including
the shop's maintenance.

## Fix

Keep cron running and make the job harmless. At the least, remove the
negative nice. Better, run it at the lowest priority
(`nice -n 19 ionice -c 3`) with fewer workers, or move it to a quiet hour, or
remove it if partners don't need a feed every minute. Talk to whoever owns
it: the comment in the cron file names a ticket.

## Curriculum

- [Useful shell tools: nice and renice](https://www.opsschool.org/shell_tools_101.html#nice-and-renice)
- [Cron 101](https://www.opsschool.org/cron_101.html)
- [Unix 101: processes](https://www.opsschool.org/unix_101.html)
