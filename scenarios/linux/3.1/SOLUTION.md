# Files hidden under a mountpoint

## What happened

During maintenance last night, `/data` was unmounted. The shop kept running,
and nothing in `shop.service` made it wait for or depend on `/data`, so it
went on writing logs (and session files) into the now-empty `/data`
directory on the root filesystem. When the volume was mounted again, it
covered those files. They still use space on `/`, but every path to them
now leads into the mounted volume, so `du` can't see them and `ls` shows the
volume's files instead.

`df` says `/` is full; `du -xsh /*` adds up to far less. `lsof +L1` shows no
deleted-but-open files, which rules out the other common cause.

## Mitigate

Look underneath the mountpoint with a bind mount of the root filesystem:

```
mount --bind / /mnt
du -sh /mnt/data/*
rm -rf /mnt/data/log /mnt/data/sessions
umount /mnt
```

Don't unmount `/data` to look: MySQL's data lives there.

## Fix

Make the services that write to `/data` require it, so they never start (or
keep running) without it:

```
systemctl edit shop.service    # and shop-worker.service
[Unit]
RequiresMountsFor=/data
```

`RequiresMountsFor=` adds `Requires=` and `After=` on the mount unit, so
the service stops when `/data` is unmounted and only starts once it is
mounted.

## Curriculum

- [Filesystems 101](https://www.opsschool.org/filesystems_101.html)
