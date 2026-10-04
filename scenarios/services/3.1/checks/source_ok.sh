#!/usr/bin/env bash
# The repository's shop.env, as the sync would install it, points at a
# payments service that answers.
set -euo pipefail
url=$(git -C /srv/git/shop-config.git show main:shop.env | sed -n 's/^SHOP_PAYMENTS_URL=//p' | tr -d '"')
if ! curl -fsS -m 5 "$url/health" >/dev/null; then
  echo "the repository's SHOP_PAYMENTS_URL ($url) does not answer"
  exit 1
fi
