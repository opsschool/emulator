# Payments unreachable

## What happened

A network config push changed the site resolver in
`/etc/systemd/resolved.conf.d/site-dns.conf` to an address where nothing
answers, and restarted the shop. The shop calls the payments service by
name, `payments.shop.internal`. Every lookup now times out, so every order
fails while browsing, which doesn't call payments, keeps working. The
shop's error only says the call timed out, after five seconds:

    Post "http://payments.shop.internal:8081/authorize": context deadline exceeded

The time goes to the lookup, not the connection: `dig
payments.shop.internal` hangs, while `curl http://127.0.0.1:8081/health`
answers at once.

`/etc/resolv.conf` points at systemd-resolved's local stub (127.0.0.53),
so the real servers only show up in `resolvectl status`. `dig` against the
configured server times out. `dig @10.53.0.10` against the site resolver
(dnsmasq on the `svc0` interface, see `ss -lunp`) answers.

## Mitigate

Pin the name in `/etc/hosts`. Checkout works again, but every other DNS
lookup on the machine is still broken, and the pin hides the problem until
the payments service moves.

## Fix

Correct the resolver at its source and restart systemd-resolved:

    sed -i 's/^DNS=.*/DNS=10.53.0.10/' /etc/systemd/resolved.conf.d/site-dns.conf
    systemctl restart systemd-resolved

Remove the `/etc/hosts` pin. `resolvectl dns` changes only last until the
next reboot. Then tell the network team which revision broke it, so the
next push doesn't put it back.

## Curriculum

- [DNS 101](https://www.opsschool.org/dns_101.html)
- [DNS 201](https://www.opsschool.org/dns_201.html)
