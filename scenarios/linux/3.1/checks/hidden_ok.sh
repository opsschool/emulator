#!/usr/bin/env bash
# Look under the mountpoint through a bind mount of the root filesystem.
set -euo pipefail
dir=$(mktemp -d)
trap 'umount "$dir" 2>/dev/null; rmdir "$dir"' EXIT
mount --bind / "$dir"
used=$(du -sxm "$dir/data" | cut -f1)
if ((used > 50)); then
  echo "${used} MB of files under /data on the root filesystem"
  exit 1
fi
