#!/usr/bin/env bash
# The shop's configuration is managed from a git repository: a timer pulls
# it every five minutes and installs any file that differs, restarting the
# services that use it. A commit changed the payments address to one that
# doesn't exist. Fixing /etc/shop/shop.env by hand lasts until the next sync.
set -euo pipefail

repo=/srv/git/shop-config.git
work=/var/lib/config-sync/shop-config
install -d /srv/git /var/lib/config-sync
git init -q --bare -b main "$repo"

# History: the current configuration, committed some weeks ago.
tmp=$(mktemp -d)
git -C "$tmp" init -q -b main
git -C "$tmp" config user.name "Wally"
git -C "$tmp" config user.email "wally@shop.internal"
cp /etc/shop/shop.env "$tmp/shop.env"
cat >"$tmp/MANIFEST" <<'MANIFEST'
# file      destination           restart
shop.env    /etc/shop/shop.env    shop.service shop-worker.service
MANIFEST
GIT_AUTHOR_DATE="$(date -d '-41 days' -R)" GIT_COMMITTER_DATE="$(date -d '-41 days' -R)" \
  git -C "$tmp" add -A
GIT_AUTHOR_DATE="$(date -d '-41 days' -R)" GIT_COMMITTER_DATE="$(date -d '-41 days' -R)" \
  git -C "$tmp" commit -q -m "Import shop configuration from the server"
# The bad change, from a developer this morning.
git -C "$tmp" config user.name "Priya Patel"
git -C "$tmp" config user.email "priya@shop.internal"
sed -i "s|^SHOP_PAYMENTS_URL=.*|SHOP_PAYMENTS_URL=$OPSSCHOOL_VAR_BAD_URL|" "$tmp/shop.env"
GIT_AUTHOR_DATE="$(date -d '-3 hours' -R)" GIT_COMMITTER_DATE="$(date -d '-3 hours' -R)" \
  git -C "$tmp" commit -q -am "payments: switch to the new gateway endpoint (PAY-301)"
git -C "$tmp" push -q "$repo" main
rm -rf "$tmp"
git clone -q "$repo" "$work"

cat >/usr/local/sbin/config-sync <<'SCRIPT'
#!/usr/bin/env bash
# Installs configuration from the shop-config repository (INFRA-12). Every
# file in MANIFEST is copied to its destination when it differs, and the
# listed services are restarted.
set -euo pipefail
work=/var/lib/config-sync/shop-config
git -C "$work" pull -q --ff-only
rev=$(git -C "$work" rev-parse --short HEAD)
grep -v '^#' "$work/MANIFEST" | while read -r file dest units; do
  [[ -n $file ]] || continue
  if ! cmp -s "$work/$file" "$dest"; then
    install -m 0644 "$work/$file" "$dest"
    # shellcheck disable=SC2086 # a list of units
    systemctl restart $units
    logger -t config-sync "installed $file at $rev, restarted $units"
  fi
done
SCRIPT
chmod 0755 /usr/local/sbin/config-sync
cat >/etc/systemd/system/config-sync.service <<'UNIT'
[Unit]
Description=Install configuration from the shop-config repository

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/config-sync
UNIT
cat >/etc/systemd/system/config-sync.timer <<'UNIT'
[Unit]
Description=Sync configuration every five minutes

[Timer]
OnCalendar=*:0/5
AccuracySec=1s

[Install]
WantedBy=timers.target
UNIT
systemctl daemon-reload
systemctl enable --now config-sync.timer
systemctl start config-sync.service
