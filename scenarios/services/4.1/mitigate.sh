#!/usr/bin/env bash
# Reference mitigation: let Redis accept writes even though it can't save.
# The disk is still full and filling.
set -euo pipefail
redis-cli config set stop-writes-on-bgsave-error no >/dev/null
