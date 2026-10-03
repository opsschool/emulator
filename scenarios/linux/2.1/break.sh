#!/usr/bin/env bash
# Two ways a filesystem can refuse writes while it looks fine. The seed picks
# one:
#   deleted-log  The shop's log has no rotation and filled /data. Someone
#                deleted it, but the shop still has it open, so the space is
#                never freed. df says full; du can't find what's using it.
#   inodes       Session cleanup was moved from hourly to weekly. Checkout
#                session files pile up until /data runs out of inodes. df -h
#                shows plenty of space.
set -euo pipefail

fill_deleted_log() {
  source /etc/shop/shop.env
  local log=$SHOP_LOG_FILE size_mb
  size_mb=$(df --output=size -m /data | tail -1 | tr -d ' ')
  # Weeks of info-level logs with no rotation, until the volume is full.
  awk -v mb="$size_mb" -v seed="$OPSSCHOOL_SEED" 'BEGIN {
      srand(seed); limit = mb * 1048576; n = 0
      while (n < limit) {
        line = sprintf("{\"time\":\"2026-09-%02dT%02d:%02d:%02d.%06dZ\",\"level\":\"INFO\",\"msg\":\"order placed\",\"component\":\"serve\",\"version\":\"2.3.0\",\"order_id\":%d,\"customer_id\":%d,\"total_cents\":%d}\n", 1 + int(rand() * 30), int(rand() * 24), int(rand() * 60), int(rand() * 60), int(rand() * 1000000), int(rand() * 9000000), int(rand() * 200000), int(rand() * 50000))
        printf "%s", line
        n += length(line)
      }
    }' >>"$log" 2>/dev/null || true
  # The on-call engineer's cleanup.
  rm -f "$log"
}

fill_sessions() {
  local dir=/data/sessions free
  sed -i -e 's/^# Nightly report and session cleanup. Runs hourly so reports stay fresh.$/# Nightly report and session cleanup. Weekly: the report query was slowing\n# the database at peak (OPS-1874)./' \
    -e 's/^17 \* \* \* \* shop /17 3 * * 0 shop /' /etc/cron.d/shop-maintenance
  free=$(df --output=iavail /data | tail -1 | tr -d ' ')
  # A week of checkout sessions that nothing cleaned up.
  awk -v n="$free" -v dir="$dir" -v seed="$OPSSCHOOL_SEED" 'BEGIN {
      srand(seed)
      for (i = 0; i < n; i++) {
        f = sprintf("%s/sess_%08x%08x%08x%08x", dir, rand() * 4294967295, rand() * 4294967295, i, rand() * 4294967295)
        printf "{\"customer_id\":%d,\"product_id\":%d,\"quantity\":%d}", 1 + int(rand() * 200000), 1 + int(rand() * 5000), 1 + int(rand() * 3) > f
        if (close(f) != 0) break
      }
    }' 2>/dev/null || true
  chown -R shop:shop "$dir" 2>/dev/null || true
  # Spread their timestamps over the last week.
  local day=0
  for p in 0 1 2 3 4 5 6 7 8 9 a b c d e f; do
    find "$dir" -maxdepth 1 -name "sess_$p*" -print0 | xargs -0 -r touch -d "$((day % 7 + 1)) days ago"
    day=$((day + 1))
  done
}

case "$OPSSCHOOL_VAR_VARIANT" in
  deleted-log) fill_deleted_log ;;
  inodes) fill_sessions ;;
  *)
    echo "unknown variant $OPSSCHOOL_VAR_VARIANT" >&2
    exit 1
    ;;
esac
