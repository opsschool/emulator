#!/usr/bin/env bash
# Passes when logrotate has a rule that covers the shop's current log file.
set -euo pipefail
source /etc/shop/shop.env
logrotate --debug /etc/logrotate.conf 2>&1 | grep -qF "considering log $SHOP_LOG_FILE"
