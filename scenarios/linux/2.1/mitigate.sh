#!/usr/bin/env bash
# Reference mitigation: free the space or inodes without fixing why they ran
# out.
set -euo pipefail
case "$OPSSCHOOL_VAR_VARIANT" in
  deleted-log)
    # Truncate the deleted log through the shop's open file descriptors.
    for pid in $(pgrep -x shop); do
      for fd in /proc/"$pid"/fd/*; do
        if [[ $(readlink "$fd") == /data/*" (deleted)" ]]; then
          truncate -s 0 "$fd"
        fi
      done
    done
    ;;
  inodes)
    find /data/sessions -maxdepth 1 -name 'sess_*' -mmin +60 -delete
    ;;
esac
systemctl restart mysql.service
