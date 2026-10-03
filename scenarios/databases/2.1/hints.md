Which route is slow? The dashboard's latency panel breaks it down by route.
---
Slow requests usually mean slow queries. MySQL's slow query log is at `/data/mysql/slow.log`.
---
Run the slow query with `EXPLAIN` in front of it. How many rows does MySQL expect to read?
---
Something changed the schema this morning. See `/var/log/shop-migrate.log` and `/opt/shop/migrations`.
