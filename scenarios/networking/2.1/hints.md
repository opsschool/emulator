nginx is up and answering with errors. What does its error log say about the upstream? `tail /var/log/nginx/error.log`.
---
Is the app itself healthy? `curl -v http://127.0.0.1:8080/health` from inside the machine.
---
The app is running and listening (`ss -ltnp`), but connections to it don't get through. What sits between two processes on the same machine? `iptables -L INPUT -n -v --line-numbers`.
---
Removing the live rule brings the shop back, but will it survive a reboot? Find where the rules are loaded from at boot.
