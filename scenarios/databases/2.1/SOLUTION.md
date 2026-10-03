# One page got slow

## What happened

A schema cleanup dropped `idx_orders_customer_created`, an index on
`orders (customer_id, created_at)`. The index-usage report that called it
unused came from a replica that never served the order history page. That
page runs:

    SELECT ... FROM orders WHERE customer_id = ? ORDER BY created_at DESC LIMIT 20

Without the index MySQL scans all three million orders for every request.
The query shows up in `/data/mysql/slow.log`, and `EXPLAIN` shows a full
table scan (`type: ALL`) with a filesort.

## Mitigate

Any index starting with `customer_id` makes the lookup fast again. An index
on `customer_id` alone still sorts each customer's whole history on every
request (`Using filesort`), which gets slower as customers place more
orders.

## Fix

Restore the index the query was written for:

    ALTER TABLE orders ADD INDEX idx_orders_customer_created (customer_id, created_at);

MySQL reads the newest 20 rows for the customer straight from the index, in
order, and stops. Drop any temporary index you added. Then revert the
migration in `/opt/shop/migrations` so it doesn't run again, and check
index usage on the primary, which serves this query, before dropping
indexes again (`sys.schema_unused_indexes`).

## Curriculum

- [Databases 201](https://www.opsschool.org/databases_201.html)
