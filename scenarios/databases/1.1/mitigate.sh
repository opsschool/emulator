#!/usr/bin/env bash
# Reference mitigation: restart the shop, which closes the leaked
# connections. 2.4.0 is still deployed and starts leaking again.
set -euo pipefail
systemctl restart shop.service
