# Root disk full, Redis refuses writes

## What happened

1. The thumbnail service was updated to the vendor's 1.5.0 release, which
   decodes large frames through a scratch file in `/var/tmp/shop-thumbs`,
   sized from the dimensions in the photo's header (THUMB-77). One of
   Wally's new photos has a corrupt header that claims 65535x65535 pixels,
   a 12 GB frame. thumbd wrote until the root disk was full, crashed on
   `No space left on device`, and left the scratch file behind. systemd
   restarts it every second, and every run does the same.
2. Redis keeps its snapshot in `/var/lib/redis`, on the root disk. It
   couldn't save, and because `stop-writes-on-bgsave-error` is on, it
   refuses writes (`MISCONF`) rather than risk losing data silently.
3. Checkout takes a lock in Redis so a double-clicked "Place order" can't
   charge twice. With Redis refusing writes, every checkout fails. Browsing
   only reads from Redis, so it still works. The shop's data volume, `/data`,
   has plenty of room, which makes the disk alert easy to dismiss.

## Mitigate

Stop the thumbnail service and delete its scratch files. Redis accepts
writes again after its next successful save (`redis-cli bgsave`). Freeing
space without stopping thumbd lasts about a minute.

## Fix

Stop the bad release from doing it again: roll `shop-thumbs` back to 1.4.2,
which rejects the photo, or quarantine the photo and keep 1.5.0 until the
vendor fixes it. Then delete the scratch files and check that Redis can
save (`redis-cli bgsave`, then `info persistence`). Leave
`stop-writes-on-bgsave-error` on: turning it off hides the next failure.
Consider giving scratch space its own filesystem.

## Curriculum

- [Filesystems 101: when a filesystem is full](https://www.opsschool.org/filesystems_101.html#when-a-filesystem-is-full)
- [Troubleshooting 101](https://www.opsschool.org/troubleshooting_101.html)
