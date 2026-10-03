# The proxy can't reach the app

## What happened

A hardening change added a firewall rule meant to make sure only the proxy
can reach the app on port 8080:

    -A INPUT -p tcp --dport 8080 -m comment --comment "app port: proxy only (SEC-2291)" -j DROP

The proxy is nginx on the same machine and connects over loopback, which
this rule also matches. With `DROP` nginx's connections hang until they
time out (504); with `REJECT` they fail at once (502). SSH and everything
else still work, because only port 8080 is affected. The rule was applied
live and saved to `/etc/iptables/rules.v4`, which `netfilter-persistent`
loads at boot.

`iptables -L INPUT -n -v` shows the rule's packet counter climbing.

## Mitigate

Delete the live rule: `iptables -D INPUT <number>`. The shop recovers at
once, but the rule returns on the next boot.

## Fix

Fix the rule at its source, `/etc/iptables/rules.v4`, and keep its intent:

    -A INPUT ! -i lo -p tcp -m tcp --dport 8080 -m comment --comment "app port: proxy only (SEC-2291)" -j DROP

then reload with `netfilter-persistent reload` or `iptables-restore <
/etc/iptables/rules.v4`. The app only listens on 127.0.0.1 anyway, so
deleting the rule entirely is also defensible. Tell the security team
what happened.

## Curriculum

- [Networking 201](https://www.opsschool.org/networking_201.html)
- [Security 101](https://www.opsschool.org/security_101.html)
