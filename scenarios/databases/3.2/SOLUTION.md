# Everything on orders hangs

## What happened

Release 2.4.0 changed the worker's reconcile loop. Once a minute it counts
orders stuck in `pending`, and when there are none it returns without
committing, so the transaction stays open on an idle connection. A
transaction that has read from a table holds a shared *metadata lock* on it
until it ends, and these never end.

The gift-note migration runs `ALTER TABLE orders ADD COLUMN gift_note`.
Adding a column at the end is instant in MySQL 8, but even an instant
change needs an exclusive metadata lock for a moment, so it waits behind
the idle transactions. MySQL queues lock requests in order: every query on
`orders` that arrives after the ALTER waits behind it, including plain
`SELECT`s that the idle transactions wouldn't block on their own.

`SHOW PROCESSLIST` shows the ALTER and a growing crowd of queries in
`Waiting for table metadata lock`, and a few `shop` connections in `Sleep`.
`information_schema.innodb_trx` shows those sleeping connections inside
transactions that started when the worker did, and
`performance_schema.metadata_locks` (or `sys.schema_table_lock_waits`)
shows who holds the lock and who waits for it.

The shop gives up on a query after five seconds, but MySQL keeps the
waiting query's thread until the lock is granted. Under load they pile up
until the shop's account reaches its connection limit (120,
`max_user_connections`), and the rest of the shop starts to fail too.
Without that limit they would fill all of MySQL's `max_connections`, and
an administrator couldn't connect either.

## Mitigate

Either end the wait or end the holders:

- Kill the ALTER (`KILL <id>`). Everything queued behind it runs at once.
  The migration hasn't happened.
- Restart `shop-worker`, which closes its idle transactions. The ALTER
  gets its lock, finishes instantly, and the queue drains.

Both work until the next schema change: 2.4.0's worker opens a new idle
transaction every minute.

## Fix

Roll back to 2.3.0 (`/opt/shop/current` points at the release) and restart
the worker and the shop, then make sure the migration has run: the column
exists, or apply `/opt/shop/migrations/*_orders_add_gift_note.sql` again.

Before running DDL on a busy table, look for long-running transactions
first, and give the migration a short `lock_wait_timeout` so it gives up and
retries instead of stopping all traffic while it waits.

## Curriculum

- [Databases 201](https://www.opsschool.org/databases_201.html)
