# Disk full, database down

## What happened

1. The thumbnail service was updated to the vendor's 1.5.0 release, which
   decodes large frames through a scratch file in `/var/tmp/shop-thumbs`,
   sized from the dimensions in the photo's header (THUMB-77). One of
   Wally's new photos has a corrupt header that claims 65535x65535 pixels,
   a 12 GB frame. thumbd wrote until the root disk was full, crashed on
   `No space left on device`, and left the scratch file behind. systemd
   restarts it every second, and every run does the same.
2. MySQL's data is on `/data`, which has plenty of space. But InnoDB keeps
   temporary files in `tmpdir`, which is `/tmp` on the root disk. A write
   there failed and mysqld aborted (`Assertion failure: os0file.cc`). It
   can't start again while it can't create its temporary files, and after a
   few attempts systemd gives up.
3. Redis can't save its snapshot to the full disk either, so it refuses
   writes (`MISCONF`), and checkout's lock fails even once MySQL is back.

## Mitigate

Stop the thumbnail service, delete its scratch files, and start MySQL
(`systemctl reset-failed mysql` first, if systemd gave up). Freeing space
without stopping thumbd lasts about a minute.

## Fix

Stop the bad release from doing it again: roll `shop-thumbs` back to 1.4.2,
which rejects the photo, or quarantine the photo and keep 1.5.0 until the
vendor fixes it. Then delete the scratch files, start MySQL, and check that
Redis can save (`redis-cli bgsave`, then `info persistence`). Consider
moving MySQL's `tmpdir` off the root disk, and putting scratch space on its
own filesystem.

## Curriculum

- [Troubleshooting 101](https://www.opsschool.org/troubleshooting_101.html)
