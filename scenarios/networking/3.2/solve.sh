#!/usr/bin/env bash
# Reference fix: remove the stale pin, or retire the old host, and let ARP
# learn the right address.
set -euo pipefail
case "$OPSSCHOOL_VAR_VARIANT" in
pinned)
  python3 - <<'PY'
import re
p = "/etc/systemd/network/br-svc.network"
s = open(p).read()
s = re.sub(r"\n# NET-301:.*?\[Neighbor\]\n[^\[]*", "\n", s, flags=re.S)
open(p, "w").write(s.rstrip() + "\n")
PY
  networkctl reload
  networkctl reconfigure br-svc
  ;;
duplicate)
  systemctl disable --now payments-old.service
  ip netns del pay-old
  ;;
esac
ip neigh del 10.54.0.20 dev br-svc 2>/dev/null || true
