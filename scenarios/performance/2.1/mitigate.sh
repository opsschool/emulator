#!/usr/bin/env bash
# Reference mitigation: stop the worker. The shop is fast again, but orders
# queue up unprocessed.
set -euo pipefail
systemctl stop shop-worker.service
