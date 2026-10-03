Orders write to the database, and browsing mostly reads from the cache. What does a database need in order to accept a write?
---
Check free space on every filesystem with `df -h`. Is one of them full?
---
Find what is using the space: `du -xh --max-depth=2 /data | sort -h | tail`.
---
Why is that file growing so fast? Look at the shop's configuration in `/etc/shop/shop.env`.
