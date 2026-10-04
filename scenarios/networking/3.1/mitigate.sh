#!/usr/bin/env bash
# Reference mitigation: raise the limit at runtime. The file still sets the
# small value at the next boot.
set -euo pipefail
sysctl -q -w net.netfilter.nf_conntrack_max=262144
