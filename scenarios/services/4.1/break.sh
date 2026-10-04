#!/usr/bin/env bash
# Version 1.5.0 of the thumbnail service decodes large frames through a
# scratch file, sized from the photo's own header. One of the new photos
# claims to be 65535x65535, a 12 GB frame: thumbd writes until the root disk
# is full, crashes on the error, and leaves the file behind. systemd restarts
# it, and it does the same again. Redis can't save its snapshot to the full
# disk, so it refuses writes, and checkout can't take its lock.
set -euo pipefail

install -d /opt/shop-thumbs/releases/1.5.0
install -m 0755 /usr/local/lib/shop-builds/thumbd-1.5.0/thumbd /opt/shop-thumbs/releases/1.5.0/thumbd
ln -sfn /opt/shop-thumbs/releases/1.5.0 /opt/shop-thumbs/current
systemctl restart shop-thumbs.service
logger -t deploy "shop-thumbs 1.5.0 (vendor release)"
sleep 5

# Wally's uploads: good photos, and one whose header claims a huge frame.
python3 - "$OPSSCHOOL_VAR_PHOTO" <<'PY'
import os, sys
q = "/data/uploads/queue"
for i, name in enumerate(["IMG_2229.jpg", "IMG_2230.jpg", sys.argv[1], "IMG_2244.jpg"]):
    w, h = (65535, 65535) if name == sys.argv[1] else (300 + i * 20, 200 + i * 10)
    with open(os.path.join(q, name), "wb") as f:
        f.write(b"\xff\xd8" + w.to_bytes(2, "big") + h.to_bytes(2, "big") + os.urandom(20000 + i * 977))
PY
chown shop:shop /data/uploads/queue/*

# Wait until the disk is full and the damage is done.
for _ in $(seq 120); do
  (($(df --output=avail -B1 / | tail -1) < 1048576)) && break
  sleep 5
done
# Redis would try to save within 5 minutes anyway; save now.
redis-cli bgsave >/dev/null 2>&1 || true
for _ in $(seq 30); do
  redis-cli info persistence | grep -q 'rdb_last_bgsave_status:err' && break
  sleep 1
done
