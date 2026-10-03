# Too many connections

## What happened

Release 2.4.0 has a bug on an error path: when an order names a product
that does not exist (customers ordering from a stale page), it returns
without rolling back the transaction. The connection stays checked out of
the pool forever, idle in MySQL with a transaction open. The release also
raised `SHOP_DB_POOL_SIZE` to 200, above MySQL's `max_connections` (151), so
instead of stalling inside its own pool the shop kept opening connections
until MySQL refused everyone:

    Error 1040 (08004): Too many connections

The health check pings the database, so it fails too.

## Mitigate

Restart the shop. That closes the leaked connections and the shop
recovers, but 2.4.0 starts leaking again with the next bad order.

## Fix

Roll back to 2.3.0:

    ln -sfn /opt/shop/releases/2.3.0 /opt/shop/current
    systemctl restart shop

and set the pool size back below `max_connections`, leaving room for the
worker, cron and exporters. In a real team you would also file the bug with
the evidence: the open transactions in `information_schema.innodb_trx` and
the order requests that triggered them.

## Curriculum

- [Databases 101](https://www.opsschool.org/databases_101.html)
- [Deployment 101](https://www.opsschool.org/deployment_101.html)
