# Crash loop after a config change

## What happened

A configuration change edited `/etc/shop/shop.env` and got one value wrong:
a letter O instead of a zero in `SHOP_DB_POOL_SIZE`, a duration without a
unit, `ture` for `true`, or `10sec` for `10s`. The shop validates its
configuration at startup and exits on a bad value. systemd restarts it every
two seconds, it exits again, and nginx returns 502 because nothing listens
on port 8080. The order worker reads the same file and is down too.

`journalctl -u shop` shows the exact problem:

    shop: invalid configuration: SHOP_DB_POOL_SIZE: "4O" is not a number

## Mitigate

Get the API running on a known-good config, for example by pointing the
unit at the backup with a drop-in, or by copying the backup back.

## Fix

Correct the value in `shop.env`, check it with
`set -a; . /etc/shop/shop.env; shop check-config`, and restart both
`shop` and `shop-worker`. Remove any temporary override so the services
read the real file again. If systemd gave up restarting
(`start request repeated too quickly`), `systemctl reset-failed` first.

## Curriculum

- [Troubleshooting 101](https://www.opsschool.org/troubleshooting_101.html)
- [Configuration management 101](https://www.opsschool.org/config_management.html)
