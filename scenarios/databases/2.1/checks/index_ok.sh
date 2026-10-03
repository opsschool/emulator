#!/usr/bin/env bash
# Passes when the order history query can read a customer's newest orders
# straight from an index: EXPLAIN shows no full scan and no filesort.
set -euo pipefail
plan=$(mysql -N -B -r shop -e "EXPLAIN FORMAT=TREE
  SELECT id, customer_id, product_id, quantity, total_cents, status, payment_ref, created_at
  FROM orders WHERE customer_id = 4242 ORDER BY created_at DESC LIMIT 20")
echo "$plan" | tr '\n' ' '
echo
if grep -qiE 'table scan|sort:' <<<"$plan"; then
  exit 1
fi
