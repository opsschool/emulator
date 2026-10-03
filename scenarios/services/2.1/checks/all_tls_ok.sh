#!/usr/bin/env bash
# Every HTTPS site must verify against the system CA store alone and must not
# expire within 14 days.
set -euo pipefail
for host in api.shop.internal partners.shop.internal; do
  if ! out=$(curl -sS -o /dev/null -m 5 --resolve "$host:443:127.0.0.1" "https://$host/health" 2>&1); then
    echo "$host: ${out%%$'\n'*}"
    exit 1
  fi
  if ! openssl s_client -connect 127.0.0.1:443 -servername "$host" </dev/null 2>/dev/null |
    openssl x509 -noout -checkend $((14 * 86400)) >/dev/null; then
    echo "$host: certificate expires within 14 days"
    exit 1
  fi
done
