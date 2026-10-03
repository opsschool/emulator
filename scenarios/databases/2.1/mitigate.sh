#!/usr/bin/env bash
# Reference mitigation: a quick index on customer_id. Lookups are fast again,
# but every request still sorts the customer's whole history.
set -euo pipefail
mysql shop -e 'ALTER TABLE orders ADD INDEX idx_orders_customer (customer_id)'
