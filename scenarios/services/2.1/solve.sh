#!/usr/bin/env bash
# Reference fix: reissue both certificates with full chains.
set -euo pipefail
/usr/local/sbin/shop-cert-issue api.shop.internal
/usr/local/sbin/shop-cert-issue partners.shop.internal
systemctl reload nginx.service
