`df -h` and `du` disagree. Which filesystem is full, and how much of it can `du -xsh /*` account for?
---
Space that `du` can't find is either in deleted files that are still open, or in files it can't reach. You can rule out the first with `lsof +L1`.
---
A mount hides whatever was in the directory before. What was mounted most recently? `findmnt`, and the journal around the maintenance.
---
To see underneath a mountpoint, bind-mount the root filesystem somewhere else: `mount --bind / /mnt` and look in `/mnt/data`.
---
Why could anything write to /data while the volume was missing? Look at what `shop.service` depends on.
