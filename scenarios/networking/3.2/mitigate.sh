#!/usr/bin/env bash
# Reference mitigation: pin the right MAC at runtime. A reboot brings back
# the bad pin, or the old host.
set -euo pipefail
ip neigh replace 10.54.0.20 lladdr 52:54:00:36:00:14 nud permanent dev br-svc
