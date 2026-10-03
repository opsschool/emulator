#!/usr/bin/env bash
# Reference mitigation: stop cron and kill the running job. That also stops
# the shop's maintenance job, and the feed job comes back when cron does.
set -euo pipefail
systemctl stop cron.service
pkill -f "/usr/local/bin/$OPSSCHOOL_VAR_JOB" || true
pkill -x gzip || true
