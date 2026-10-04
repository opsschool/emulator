# Redis refuses writes

## What happened

Three things went wrong in a row.

1. The thumbnail service was updated to the vendor's 1.5.0 release, which
   reads part of each photo without checking that it stays inside the file
   (THUMB-77). One of Wally's new photos has a header that points far past
   its end, so `thumbd` crashes with a segmentation fault. systemd restarts
   it every second, and it crashes on the same photo every time.
2. The vendor asked for every crash dump, uncompressed and complete
   (`/etc/systemd/coredump.conf.d/50-thumbs-vendor.conf`), and the settings
   also removed systemd-coredump's limits (`MaxUse=1T`, `KeepFree=0`). Each
   dump is about 150 MB, and they filled the root disk within minutes.
3. Redis writes its snapshot to `/var/lib/redis` on the root disk. When a
   save failed, Redis did what it is configured to do
   (`stop-writes-on-bgsave-error yes`) and refused all writes, with
   `MISCONF` errors. Checkout takes a short lock in Redis so a customer
   can't be charged twice, and it fails closed: no lock, no order.

## Mitigate

Get checkout working: `redis-cli config set stop-writes-on-bgsave-error no`,
or free space on the disk. Freeing space alone lasts only minutes while
`thumbd` keeps crashing.

## Fix

- Stop the crash loop: roll `shop-thumbs` back to 1.4.2, which rejects the
  bad photo, or move the photo out of the queue.
- Put the core dump limits back (remove `MaxUse` and `KeepFree` from the
  vendor's file, or set sensible ones), then delete the old dumps.
- Turn `stop-writes-on-bgsave-error` back on if you turned it off, and make
  sure Redis can save (`redis-cli bgsave`, then `info persistence`).

## Curriculum

- [Troubleshooting 101](https://www.opsschool.org/troubleshooting_101.html)
