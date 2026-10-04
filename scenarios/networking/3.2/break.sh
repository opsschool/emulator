#!/usr/bin/env bash
# The shop reaches the payments host (10.54.0.20, MAC 52:54:00:36:00:14) as a
# neighbor on the br-svc segment, so it needs the right MAC address for it.
#   pinned:    a static neighbor entry, left from a NIC replacement, has two
#              digits of the MAC swapped. Packets go to a MAC nobody has.
#   duplicate: the old payments box was kept running on the same segment
#              with the same IP. It announces itself every 20 seconds, so the
#              shop's ARP entry flips between the two hosts.
set -euo pipefail

case "$OPSSCHOOL_VAR_VARIANT" in
pinned)
  cat >>/etc/systemd/network/br-svc.network <<'CONF'

# NET-301: pin the payments host while its NIC is replaced. Remove after
# the swap.
[Neighbor]
Address=10.54.0.20
LinkLayerAddress=52:54:00:36:00:41
CONF
  networkctl reload
  networkctl reconfigure br-svc
  ;;
duplicate)
  cat >/usr/local/sbin/payments-old-up <<'SCRIPT'
#!/usr/bin/env bash
# The previous payments host, kept for the PAY-188 audit. Its address was
# reused by the new host.
set -euo pipefail
ns=pay-old
[[ -e /run/netns/$ns ]] || ip netns add "$ns"
if ! ip link show veth-pay-old >/dev/null 2>&1; then
  ip link add veth-pay-old type veth peer name ens4 netns "$ns"
fi
ip -n "$ns" link set ens4 address 52:54:00:36:00:99
ip link set veth-pay-old master br-svc up
ip -n "$ns" addr replace 10.54.0.20/24 dev ens4
ip -n "$ns" link set lo up
ip -n "$ns" link set ens4 up
SCRIPT
  chmod 0755 /usr/local/sbin/payments-old-up
  cat >/etc/systemd/system/payments-old.service <<'UNIT'
[Unit]
Description=Previous payments host (kept for PAY-188)
After=svc-net.service
Requires=svc-net.service

[Service]
ExecStartPre=/usr/local/sbin/payments-old-up
# Its HA agent announces the address it holds.
ExecStart=/bin/sh -c 'while :; do ip netns exec pay-old arping -q -U -c 1 -I ens4 10.54.0.20; sleep 20; done'
Restart=always

[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  systemctl enable --now payments-old.service
  ;;
esac
logger -t netops "NET-301: service segment work complete"
