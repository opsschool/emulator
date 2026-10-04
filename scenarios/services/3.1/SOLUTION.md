# Config sync reverts your fix

## What happened

The shop's configuration is managed from a git repository,
`/srv/git/shop-config.git`. A systemd timer, `config-sync.timer`, runs
`/usr/local/sbin/config-sync` every five minutes: it pulls the repository,
and installs every file listed in `MANIFEST` that differs from the copy on
the server, restarting the services that use it.

This morning a developer committed a change that points `SHOP_PAYMENTS_URL`
at a new payments gateway that doesn't exist yet. The sync installed it and
restarted the shop, and every checkout has failed since.

Fixing `/etc/shop/shop.env` by hand works until the next sync, which
installs the repository's copy again and restarts the shop. The journal shows
`config-sync` lines right before each restart.

## Mitigate

Stop the sync (`systemctl stop config-sync.timer`), then fix
`/etc/shop/shop.env` and restart the shop.

## Fix

Fix the source of truth: clone the repository, revert or correct the
change, and push. Then start the timer again, so the server stays managed,
and tell the developer why the change was reverted.

```
git clone /srv/git/shop-config.git /tmp/shop-config
cd /tmp/shop-config
git log -p shop.env
git revert <commit>          # or edit and commit
git push
systemctl start config-sync.timer
```

## Curriculum

- [Configuration management](https://www.opsschool.org/config_management.html)
