#!/usr/bin/python3
"""shop-thumbs: makes catalog thumbnails from uploaded product photos.

Photos land in /data/uploads/queue. Each one is decoded, and its thumbnail
is written to /data/uploads/thumbs. Photos that can't be decoded go to
/data/uploads/rejected.
"""
import ctypes
import logging
import os
import sys
import time

VERSION = "@VERSION@"
# 1.5.0 reads the segment table in place instead of copying it (THUMB-77).
ZERO_COPY = @ZERO_COPY@

QUEUE = "/data/uploads/queue"
THUMBS = "/data/uploads/thumbs"
REJECTED = "/data/uploads/rejected"

log = logging.getLogger("thumbd")

# Decode buffers are allocated and touched once at start, so decoding never
# waits on page faults.
POOL = bytearray(os.urandom(128 << 20))


class BadPhoto(Exception):
    pass


def segment_table(data):
    """Returns the 16-byte segment table that the photo's header points to."""
    if data[:2] != b"\xff\xd8":
        raise BadPhoto("not a JPEG")
    off = int.from_bytes(data[2:6], "big")
    if ZERO_COPY:
        buf = (ctypes.c_char * len(data)).from_buffer_copy(data)
        return ctypes.string_at(ctypes.addressof(buf) + off, 16)
    if off + 16 > len(data):
        raise BadPhoto(f"segment table at {off} is past the end of the file")
    return data[off:off + 16]


def thumbnail(path):
    with open(path, "rb") as f:
        data = f.read()
    table = segment_table(data)
    n = len(data) % len(POOL)
    POOL[n:n + 16] = table
    name = os.path.basename(path)
    with open(os.path.join(THUMBS, name), "wb") as f:
        f.write(data[:2] + table)
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
