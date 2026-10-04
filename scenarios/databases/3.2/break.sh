#!/usr/bin/env bash
# A hardening drop-in caps the number of tasks (threads) the shop may run.
# Go starts an OS thread for every goroutine blocked in a system call, such
# as the fsync of each checkout session file. At peak the shop needs more
# threads than the cap, the runtime can't create one, and it crashes.
set -euo pipefail
install -d /etc/systemd/system/shop.service.d
cat >/etc/systemd/system/shop.service.d/60-hardening.conf <<CONF
# SEC-212: limit runaway processes. Sized from the shop's normal thread
# count.
[Service]
TasksMax=$OPSSCHOOL_VAR_TASKS
CONF
systemctl daemon-reload
systemctl restart shop.service
