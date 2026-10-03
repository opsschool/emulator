#!/usr/bin/env bash
# Reference mitigation: serve the API certificate with its intermediate.
# The partners certificate still expires in a few days.
set -euo pipefail
crt=/etc/nginx/tls/api.shop.internal.crt
cat "$crt" /etc/ssl/shop-ca/intermediate.crt >"$crt.new"
mv "$crt.new" "$crt"
systemctl reload nginx.service
