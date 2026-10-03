What error do failed orders log? `journalctl -u shop -p err -n 20`. A write is failing; why?
---
Compare `df -h /data` with what `du -sh /data/*` can find, and look at `df -i /data` as well. Which number doesn't add up?
---
If `df` and `du` disagree, a process is holding a deleted file open: `lsof +L1`. If inodes are exhausted, find the directory with the most files: `for d in /data/*; do echo "$(find "$d" | wc -l) $d"; done`.
---
Freeing the space or the inodes gets checkout working. Then work out why they filled up, and make sure it can't happen again: look at how the shop's logs are rotated, and how often old sessions are cleaned up (`/etc/cron.d`).
