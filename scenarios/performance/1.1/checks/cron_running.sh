#!/usr/bin/env bash
# Scheduled jobs, such as the shop's maintenance, must keep running.
set -euo pipefail
if ! state=$(systemctl is-active cron.service); then
  echo "cron is $state"
  exit 1
fi
