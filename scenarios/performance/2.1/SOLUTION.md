# Everything is slow

## What happened

To clear an order backlog, someone raised `SHOP_WORKER_CONCURRENCY` from 4
to 16 or 17. Each worker goroutine keeps a 256 MB invoice render buffer
(`SHOP_WORKER_BUFFER_MB`), so the worker went from 1 GB to over 4 GB on a
machine with 4 GB of RAM, alongside MySQL's 1 GB buffer pool.

The kernel kept everything running by swapping. Pages of the shop, MySQL
and the worker itself were pushed to swap and pulled back on demand, so any
request could stall for disk reads. CPU looks idle because everything is
waiting on I/O. `vmstat 1` shows thousands of pages per second in the `si`
and `so` columns. `free -m` shows almost no available memory and swap in
use.

## Mitigate

Stop the worker, or lower its concurrency. Stopping it makes the shop fast
again, but orders pile up in the queue (`shop_worker_queue_depth`).

## Fix

Size concurrency to the memory you have:
`concurrency × buffer` must fit alongside MySQL and the shop with room to
spare. Four workers × 256 MB = 1 GB fits; 16 doesn't. Restart the worker
and let it drain the queue. To process orders faster you need more memory
or smaller buffers, not more workers.

## Curriculum

- [Capacity planning](https://www.opsschool.org/capacity_planning.html)
- [Statistics: diagnosing](https://www.opsschool.org/stats_diagnosing.html)
