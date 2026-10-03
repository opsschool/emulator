#!/usr/bin/env bash
# A new cron job builds a compressed product feed every minute. It runs two
# CPU-bound compressors per core at raised priority (nice -10, so the feed
# is "never late"), and starves the shop of CPU for most of every minute.
set -euo pipefail

job="$OPSSCHOOL_VAR_JOB"
cat >"/usr/local/bin/$job" <<'SCRIPT'
#!/usr/bin/env bash
# Builds the compressed product feed for marketplace partners.
set -euo pipefail
workers=$(($(nproc) * 2))
for _ in $(seq "$workers"); do
  head -c 2G /dev/urandom | gzip -9 >/dev/null &
done
wait
SCRIPT
chmod 0755 "/usr/local/bin/$job"
cat >"/etc/cron.d/$job" <<CRON
# Partner product feed. Partners poll it every minute, so it must never
# fall behind (MKT-118).
* * * * * root timeout 55 nice -n -10 /usr/local/bin/$job >/dev/null 2>&1
CRON
