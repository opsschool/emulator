#!/usr/bin/env bash
# Reference mitigation: lift the limit on the live cgroup only. It is lost
# when the slice is restarted or the machine reboots.
set -euo pipefail
echo "max 100000" >"/sys/fs/cgroup/$OPSSCHOOL_VAR_SLICE.slice/cpu.max"
