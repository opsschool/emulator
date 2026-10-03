# Phantom disk

There are two variants, picked at random. Both make writes to `/data` fail
while the obvious checks say it should be fine.

## Variant A: a deleted file still open

### What happened

Nothing rotates the shop's log, so it grew until it filled `/data`. The
on-call engineer deleted it with `rm`. Deleting a file only removes its
name. The shop and the worker still have it open, so the kernel can't free
its blocks. `df` still shows `/data` full, `du` can't find anything big,
and MySQL and the checkout session writes keep failing.

`lsof -a +L1 /data` lists open files with no remaining links: the shop
holding gigabytes in `app.log (deleted)`.

### Mitigate

Truncate the file through the process's file descriptor:
`: > /proc/<pid>/fd/<fd>`. Or restart the shop and the worker so they
close it.

### Fix

Restart the shop and the worker so they reopen their log, and add log
rotation that can't leave them writing to a deleted file. The shop doesn't
reopen its log on a signal, so use `copytruncate`, or a `postrotate` that
restarts it.

## Variant B: out of inodes

### What happened

Every checkout writes a small session file to `/data/sessions`. The
maintenance job deletes ones older than an hour, but someone moved it from
hourly to weekly to keep its report query away from peak traffic. A week of
sessions used every inode on `/data`. Creating a file needs a free inode, so
session writes fail with "no space left on device" while `df -h` shows
gigabytes free. `df -i` shows 100% inode use.

### Mitigate

Delete old session files: `find /data/sessions -name 'sess_*' -mmin +60 -delete`.

### Fix

Clean up sessions at least hourly again. Restore the hourly schedule, or
split the cleanup into its own hourly job and run the report at a quiet
time.

## Curriculum

- [Filesystems 101](https://www.opsschool.org/filesystems_101.html)
- [Logs 101](https://www.opsschool.org/logs_101.html)
- [Cron 101](https://www.opsschool.org/cron_101.html)
