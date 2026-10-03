#!/usr/bin/env bash
# Reference fix: keep the job but run it at the lowest CPU and I/O priority
# with one worker, so the shop always wins.
set -euo pipefail
job="$OPSSCHOOL_VAR_JOB"
sed -i "s|root timeout 55 nice -n -10 /usr/local/bin/$job|root timeout 55 nice -n 19 ionice -c 3 /usr/local/bin/$job|" "/etc/cron.d/$job"
sed -i 's/^workers=.*/workers=1/' "/usr/local/bin/$job"
pkill -f "/usr/local/bin/$job" || true
pkill -x gzip || true
systemctl start cron.service
