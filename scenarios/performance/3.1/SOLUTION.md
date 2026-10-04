# CPU throttled

## What happened

A cost-control rollout (FIN-207) moved `shop.service` and
`shop-worker.service` into a new systemd slice and gave the slice a CPU
quota of 30–40% of one CPU. The quota is enforced by the CPU controller of
cgroup v2: once the processes in the slice have used their share of each
100 ms period, the kernel stops scheduling them until the next period. The
host itself stays mostly idle, so CPU graphs and `top` look healthy.

The shop and the worker share the quota. The worker's invoice rendering uses
a good part of it, so the shop is throttled for tens of milliseconds at a
time. Latency rises, worst at peak load.

The evidence is in the cgroup: `cpu.stat` in the slice's directory under
`/sys/fs/cgroup` shows `nr_throttled` and `throttled_usec` climbing.
`systemctl status shop` names the slice, `systemctl cat shop` shows the
drop-in that moved it, and `systemctl cat <slice>` shows the quota.

## Mitigate

Lift the limit on the live cgroup: write `max 100000` to the slice's
`cpu.max`, or `systemctl set-property --runtime <slice> CPUQuota=`. Both
are lost on a restart of the slice or a reboot.

## Fix

Remove or raise the quota in the slice's unit file and reload systemd.
`systemctl set-property` without `--runtime` also persists. Then tell
whoever owns FIN-207: a quota sized from average use throttles every burst,
and a service that shares a quota with a batch worker competes with it.

## Curriculum

- [Capacity planning](https://www.opsschool.org/capacity_planning.html)
