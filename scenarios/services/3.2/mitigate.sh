#!/usr/bin/env bash
# Reference mitigation: lift the limit on the running unit only.
set -euo pipefail
echo max >/sys/fs/cgroup/system.slice/shop.service/pids.max
