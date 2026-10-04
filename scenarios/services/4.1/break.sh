#!/usr/bin/env bash
# Three things go wrong in a row. Version 1.5.0 of the thumbnail service
# crashes on one of the new photos, and systemd restarts it every second.
# The vendor's core dump settings keep every crash dump, uncompressed, with
# no limit, so the dumps fill the root disk in minutes. Redis can't write
# its snapshot to the full disk and, as configured, refuses writes. Checkout
# takes a lock in Redis and fails closed.
set -euo pipefail

install -d /etc/systemd/coredump.conf.d
cat >/etc/systemd/coredump.conf.d/50-thumbs-vendor.conf <<'CONF'
# VND-31: the thumbnail vendor wants every crash dump, uncompressed and
# complete, until they ship the fix for THUMB-77.
[Coredump]
Compress=no
ProcessSizeMax=1G
ExternalSizeMax=1G
MaxUse=1T
KeepFree=0
CONF

install -d /opt/shop-thumbs/releases/1.5.0
install -m 0755 /usr/local/lib/shop-builds/thumbd-1.5.0/thumbd /opt/shop-thumbs/releases/1.5.0/thumbd
ln -sfn /opt/shop-thumbs/releases/1.5.0 /opt/shop-thumbs/current
systemctl restart shop-thumbs.service
logger -t deploy "shop-thumbs 1.5.0 (vendor release)"
sleep 5

# Wally's uploads: good photos, and one whose header points past its end.
python3 - "$OPSSCHOOL_VAR_PHOTO" <<'PY'
import os, sys
q = "/data/uploads/queue"
for i, name in enumerate(["IMG_2229.jpg", "IMG_2230.jpg", sys.argv[1], "IMG_2244.jpg"]):
    off = 0xFFF00000 if name == sys.argv[1] else 6
    with open(os.path.join(q, name), "wb") as f:
        f.write(b"\xff\xd8" + off.to_bytes(4, "big") + os.urandom(20000 + i * 977))
PY
chown shop:shop /data/uploads/queue/*

# Wait until the dumps have filled the disk and Redis has failed a save.
for _ in $(seq 120); do
  (($(df --output=avail -B1M / | tail -1) < 100)) && break
  sleep 5
done
redis-cli bgsave >/dev/null || true
for _ in $(seq 30); do
  redis-cli info persistence | grep -q 'rdb_last_bgsave_status:err' && break
  sleep 2
done
