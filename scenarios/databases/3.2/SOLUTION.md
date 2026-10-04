# Thread limit

## What happened

A hardening drop-in (`/etc/systemd/system/shop.service.d/60-hardening.conf`)
set `TasksMax` on the shop to a little above its normal thread count. In
cgroup v2 that becomes `pids.max`, and every thread counts as a task.

The Go runtime keeps a few threads for running goroutines, and starts more
whenever goroutines block in system calls. Checkout writes and `fsync`s a
session file for every order, so at peak several goroutines are in `fsync`
at once and the runtime needs more threads. When it can't create one, it
crashes: `runtime: failed to create new OS thread` and
`pthread_create failed: Resource temporarily unavailable`. systemd restarts
it, and it crashes at the next busy moment.

## Mitigate

Raise the limit on the running unit: `systemctl set-property --runtime
shop.service TasksMax=infinity`, or write `max` to its `pids.max`.

## Fix

Keep the hardening but size it with room to grow, in the drop-in, and
reload systemd. A task limit protects the host from a runaway process; it
should be far above anything the service does in normal operation,
including at peak.

## Curriculum

- [Unix 101](https://www.opsschool.org/unix_101.html)
