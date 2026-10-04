Can the shop's machine reach the payments host at all? Try `curl` against its name and its address, and `ping` it. Then do the same from the payments host's side: `ip netns exec pay1 ...`.
---
Packets to a neighbor on the same network go to its MAC address, which the machine learns with ARP. What does it think the payments host's MAC is? `ip neigh show`.
---
Compare that with the payments host's real MAC: `ip -n pay1 link show eth0`.
---
Watch who answers for the address: `tcpdump -eni br-svc arp`, or `arping -I br-svc 10.54.0.20`. Where did the wrong answer come from, and what will bring it back after a reboot?
