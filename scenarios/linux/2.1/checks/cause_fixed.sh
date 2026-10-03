#!/usr/bin/env bash
# Variant-specific: see break.sh.
set -euo pipefail

held_deleted() { # shop processes holding deleted files on /data
  lsof -nP -a +L1 /data 2>/dev/null | awk 'NR > 1 && $1 == "shop" && $8 == 0'
}

case "$OPSSCHOOL_VAR_VARIANT" in
  deleted-log)
    if [[ -n $(held_deleted) ]]; then
      echo "the shop still has a deleted file open on /data"
      exit 1
    fi
    # Force the shop's log rotation, as logrotate will do on its own, and
    # make sure the shop doesn't end up writing to a deleted file.
    mapfile -t confs < <(grep -lE '/data/log/shop' /etc/logrotate.d/* /etc/logrotate.conf 2>/dev/null || true)
    if ((${#confs[@]} == 0)); then
      echo "nothing rotates the shop's logs in /data/log/shop"
      exit 1
    fi
    state=$(mktemp)
    trap 'rm -f "$state"' EXIT
    logrotate -f -s "$state" "${confs[@]}"
    sleep 5
    if [[ -n $(held_deleted) ]]; then
      echo "after rotation the shop is writing to a deleted file"
      exit 1
    fi
    ;;
  inodes)
    # Some cron job must clean up sessions at least hourly: the shop's
    # maintenance job, or a dedicated cleanup.
    found=false
    while read -r line; do
      [[ -z "$line" || "$line" == \#* || "$line" =~ ^[A-Za-z_]+= ]] && continue
      read -r minute hour _ <<<"$line"
      if [[ "$minute" == @hourly || "$hour" == "*" || "$hour" == "*/1" ]] &&
        [[ "$line" == *"shop maintenance"* || "$line" == *sessions* ]]; then
        found=true
      fi
    done < <(cat /etc/crontab /etc/cron.d/* /var/spool/cron/crontabs/* 2>/dev/null)
    $found || { echo "no cron job cleans up /data/sessions at least hourly"; exit 1; }
    ;;
esac
