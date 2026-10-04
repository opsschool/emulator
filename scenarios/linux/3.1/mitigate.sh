#!/usr/bin/env bash
# Reference mitigation: find the files under the mountpoint and remove them.
# The shop can still start without /data next time.
set -euo pipefail
dir=$(mktemp -d)
mount --bind / "$dir"
rm -rf "${dir:?}/data/log" "${dir:?}/data/sessions"
umount "$dir"
rmdir "$dir"
