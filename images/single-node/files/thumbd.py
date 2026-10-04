#!/usr/bin/python3
"""shop-thumbs: makes catalog thumbnails from uploaded product photos.

Photos land in /data/uploads/queue. Each one is decoded, and its thumbnail
is written to /data/uploads/thumbs. Photos that can't be decoded go to
/data/uploads/rejected.
"""
import logging
import os
import sys
import tempfile
import time

VERSION = "@VERSION@"
# 1.5.0 decodes large frames through a scratch file instead of memory, to
# keep thumbd's memory flat (THUMB-77).
SPILL = @SPILL@

QUEUE = "/data/uploads/queue"
THUMBS = "/data/uploads/thumbs"
REJECTED = "/data/uploads/rejected"
SCRATCH = "/var/tmp/shop-thumbs"

log = logging.getLogger("thumbd")

# Decode buffer, allocated once at start.
POOL = bytearray(os.urandom(8 << 20))


class BadPhoto(Exception):
    pass


def decode(data):
    """Decodes the photo's frame and returns its first 16 bytes."""
    if data[:2] != b"\xff\xd8":
        raise BadPhoto("not a JPEG")
    width = int.from_bytes(data[2:4], "big")
    height = int.from_bytes(data[4:6], "big")
    frame = width * height * 3
    if not SPILL:
        if frame > 64 * len(data):
            raise BadPhoto(f"{width}x{height} is implausible for a {len(data)}-byte file")
        return bytes(POOL[:16])
    os.makedirs(SCRATCH, exist_ok=True)
    scratch = tempfile.NamedTemporaryFile(dir=SCRATCH, prefix="frame-", delete=False)
    with scratch:
        left = frame
        while left > 0:
            n = min(left, len(POOL))
            scratch.write(POOL[:n])
            left -= n
    os.remove(scratch.name)
    return bytes(POOL[:16])


def thumbnail(path):
    with open(path, "rb") as f:
        data = f.read()
    head = decode(data)
    name = os.path.basename(path)
    with open(os.path.join(THUMBS, name), "wb") as f:
        f.write(data[:2] + head)
    os.remove(path)
    log.info("thumbnail %s (%d bytes)", name, len(data))


def main():
    logging.basicConfig(level=logging.INFO, stream=sys.stdout, format="%(levelname)s %(message)s")
    for d in (QUEUE, THUMBS, REJECTED):
        os.makedirs(d, exist_ok=True)
    log.info("shop-thumbs %s watching %s", VERSION, QUEUE)
    while True:
        for name in sorted(os.listdir(QUEUE)):
            path = os.path.join(QUEUE, name)
            log.info("processing %s", name)
            try:
                thumbnail(path)
            except BadPhoto as e:
                log.warning("rejected %s: %s", name, e)
                os.replace(path, os.path.join(REJECTED, name))
        time.sleep(1)


if __name__ == "__main__":
    main()
