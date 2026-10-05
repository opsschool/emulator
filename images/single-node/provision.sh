#!/usr/bin/env bash
# Provisions the single-node scenario VM. Runs once, as root, inside the VM
# when the image is built. $1 is the build directory copied in by build.sh:
# it holds this script, files/, versions.env and the shop binaries.
#
# Expects Ubuntu: 26.04 in the VM (MySQL 8.4), 24.04 in the container
# (MySQL 8.0).
# OPSSCHOOL_PROVISION=container adapts it for the container driver: the
# exporters and Alloy are already in the image.
set -euo pipefail

build=${1:?usage: provision.sh <build-dir>}
files="$build/files"
# shellcheck source=versions.env
source "$build/versions.env"
arch=$(dpkg --print-architecture) # amd64 or arm64
export DEBIAN_FRONTEND=noninteractive
mode=${OPSSCHOOL_PROVISION:-vm}

log() { echo "==> $*"; }

install_packages() {
  log "packages"
  apt-get update -q
  # Persisted firewall rules are written by configure_firewall, not saved
  # from whatever is loaded at install time.
  echo "iptables-persistent iptables-persistent/autosave_v4 boolean false" | debconf-set-selections
  echo "iptables-persistent iptables-persistent/autosave_v6 boolean false" | debconf-set-selections
  apt-get install -y -q --no-install-recommends \
    ca-certificates curl gnupg unzip jq openssl \
    nginx redis-server cron logrotate iptables-persistent \
    strace lsof sysstat tcpdump bind9-dnsutils iproute2 iptables htop procps psmisc \
    net-tools ncat less vim-tiny python3 iputils-arping conntrack git
  # Tracing tools vary by distribution; install what exists.
  for pkg in bpftrace linux-tools-generic; do
    apt-get install -y -q --no-install-recommends "$pkg" || echo "skipping $pkg"
  done
}

install_mysql() {
  log "mysql (distribution package)"
  apt-get install -y -q mysql-server
  mysqld --version
}

# /data is a separate filesystem, as on many production hosts. It holds the
# MySQL data directory, the shop's logs and its session files.
setup_data_volume() {
  log "data volume"
  if ! grep -q ' /data ' /etc/fstab; then
    # The container driver commits this file into an image, so keep it
    # smaller there; the seeded data needs about 1.5 GB.
    if [[ $mode == container ]]; then
      truncate -s 3G /var/lib/data.img
    else
      fallocate -l 6G /var/lib/data.img
    fi
    # Discards, and the zeroing that lazy init does after mounting, punch
    # holes in the image file; then writes to /data need space on /, and a
    # full / breaks /data too. Do all zeroing now, without discarding.
    mkfs.ext4 -q -F -E nodiscard,lazy_itable_init=0,lazy_journal_init=0 -L shopdata /var/lib/data.img
    mkdir -p /data
    echo '/var/lib/data.img /data ext4 loop,defaults 0 2' >>/etc/fstab
  fi
  mountpoint -q /data || mount /data
  if [[ $mode != container ]]; then
    # Keep the image file fully allocated, like a real disk: fill any holes,
    # and don't let the weekly fstrim punch new ones.
    fallocate -l 6G /var/lib/data.img
    systemctl mask fstrim.timer
  fi
}

configure_mysql() {
  log "mysql config"
  systemctl stop mysql
  if [[ ! -d /data/mysql ]]; then
    cp -a /var/lib/mysql /data/mysql
    chown -R mysql:mysql /data/mysql
  fi
  install -m 0644 "$files/mysql-shop.cnf" /etc/mysql/mysql.conf.d/zz-shop.cnf
  # Ubuntu's AppArmor profile for mysqld only allows /var/lib/mysql.
  if [[ -f /etc/apparmor.d/tunables/alias ]] && ! grep -q '/data/mysql' /etc/apparmor.d/tunables/alias; then
    echo 'alias /var/lib/mysql/ -> /data/mysql/,' >>/etc/apparmor.d/tunables/alias
    systemctl reload apparmor 2>/dev/null || true
  fi
  systemctl start mysql
  mysql <<'SQL'
CREATE DATABASE IF NOT EXISTS shop;
CREATE USER IF NOT EXISTS 'shop'@'localhost' IDENTIFIED BY 'shop';
GRANT ALL ON shop.* TO 'shop'@'localhost';
CREATE USER IF NOT EXISTS 'exporter'@'localhost' IDENTIFIED BY 'exporter' WITH MAX_USER_CONNECTIONS 3;
GRANT PROCESS, REPLICATION CLIENT, SELECT ON *.* TO 'exporter'@'localhost';
SQL
}

# The initrd doesn't need the network: the root disk is local. If the initrd
# brings the network card up, cloud-init can't rename it to eth0 on a
# session's first boot (each clone has a new MAC address), and
# systemd-networkd-wait-online waits its full two minutes for eth0 before
# the boot goes on. See docs/decisions.md.
initrd_without_network() {
  [[ $mode == container ]] && return # no initrd
  command -v dracut >/dev/null || return
  log "initrd without networking"
  cat >/etc/dracut.conf.d/90-opsschool-no-network.conf <<'CONF'
omit_dracutmodules+=" dyn-netconf network network-legacy network-manager systemd-networkd connman "
CONF
  dracut --force --regenerate-all
}

# Swap, as on most general-purpose hosts. performance/2.1 depends on it.
setup_swap() {
  [[ $mode == container ]] && return # the container shares the host's memory
  log "swap"
  if ! grep -q ' swap ' /etc/fstab; then
    fallocate -l 2G /var/lib/swapfile
    chmod 0600 /var/lib/swapfile
    mkswap -q /var/lib/swapfile
    echo '/var/lib/swapfile none swap sw 0 0' >>/etc/fstab
  fi
  swapon -a
}

# The site resolver: dnsmasq answers for shop.internal on a service-side
# dummy interface (svc0, 10.53.0.10). systemd-resolved sends shop.internal
# lookups there and everything else to the network's own resolver.
# networking/1.1 breaks this path.
configure_site_dns() {
  [[ $mode == container ]] && return # Docker owns resolv.conf; see decisions.md
  log "site dns"
  install -m 0644 "$files/svc0.netdev" "$files/svc0.network" /etc/systemd/network/
  networkctl reload
  install -d /etc/dnsmasq.d
  install -m 0644 "$files/dnsmasq-site.conf" /etc/dnsmasq.d/site.conf
  apt-get install -y -q --no-install-recommends dnsmasq
  # dnsmasq only serves the site zone here. Keep the package's helper from handing
  # it a resolv file and registering 127.0.0.1 with resolvconf: both log
  # warnings at every boot that look like DNS trouble.
  sed -i 's/^#IGNORE_RESOLVCONF=yes/IGNORE_RESOLVCONF=yes/; s/^#DNSMASQ_EXCEPT="lo"/DNSMASQ_EXCEPT="lo"/' /etc/default/dnsmasq
  install -d /etc/systemd/resolved.conf.d
  install -m 0644 "$files/resolved-site-dns.conf" /etc/systemd/resolved.conf.d/site-dns.conf
  systemctl restart systemd-resolved
  systemctl enable dnsmasq
  systemctl restart dnsmasq
}

# The service segment: the payments host is a network namespace (pay1,
# 10.54.0.20) cabled to the br-svc bridge (10.54.0.1), so the shop reaches
# it as a real neighbor, over ARP. The container driver keeps payments on
# loopback.
configure_service_segment() {
  [[ $mode == container ]] && return
  log "service segment"
  install -m 0644 "$files/br-svc.netdev" "$files/br-svc.network" /etc/systemd/network/
  networkctl reload
  install -m 0755 "$files/svc-net-up" /usr/local/sbin/svc-net-up
  install -m 0644 "$files/svc-net.service" /etc/systemd/system/
  install -D -m 0644 "$files/shop-payments-netns.conf" /etc/systemd/system/shop-payments.service.d/netns.conf
  systemctl daemon-reload
  systemctl enable --now svc-net.service
}

# Base firewall: the database and cache are reachable only locally. Loaded
# at boot by netfilter-persistent. networking/2.1 breaks this.
configure_firewall() {
  log "firewall"
  install -d /etc/iptables
  install -m 0640 "$files/rules.v4" /etc/iptables/rules.v4
  systemctl enable netfilter-persistent
  iptables-restore </etc/iptables/rules.v4
}

# TLS for api.shop.internal and partners.shop.internal, issued by an
# internal CA that lives on this machine (as it would on a CA host). The root
# is in the system trust store; the intermediate is not, as on most clients.
configure_tls() {
  log "tls"
  local ca=/etc/ssl/shop-ca
  install -d -m 0700 "$ca"
  install -m 0755 "$files/shop-cert-issue" /usr/local/sbin/shop-cert-issue
  if [[ ! -f "$ca/intermediate.crt" ]]; then
    openssl req -x509 -newkey rsa:3072 -nodes -keyout "$ca/root.key" -out "$ca/root.crt" \
      -days 3650 -subj "/O=Shop Internal/CN=Shop Internal Root CA" \
      -addext "basicConstraints=critical,CA:true" -addext "keyUsage=critical,keyCertSign,cRLSign"
    openssl req -newkey rsa:3072 -nodes -keyout "$ca/intermediate.key" -out "$ca/intermediate.csr" \
      -subj "/O=Shop Internal/CN=Shop Internal Issuing CA 1"
    openssl x509 -req -in "$ca/intermediate.csr" -CA "$ca/root.crt" -CAkey "$ca/root.key" \
      -CAcreateserial -out "$ca/intermediate.crt" -days 1825 \
      -extfile <(printf 'basicConstraints=critical,CA:true,pathlen:0\nkeyUsage=critical,keyCertSign,cRLSign\n')
    rm -f "$ca/intermediate.csr"
  fi
  install -m 0644 "$ca/root.crt" /usr/local/share/ca-certificates/shop-internal-root-ca.crt
  update-ca-certificates
  for host in api.shop.internal partners.shop.internal; do
    [[ -f "/etc/nginx/tls/$host.crt" ]] || /usr/local/sbin/shop-cert-issue "$host"
  done
  install -m 0644 "$files/nginx-shop-tls.conf" /etc/nginx/sites-available/shop-tls
  ln -sfn /etc/nginx/sites-available/shop-tls /etc/nginx/sites-enabled/shop-tls
}

install_shop() {
  log "shop $SHOP_VERSION"
  id shop >/dev/null 2>&1 || useradd --system --home /opt/shop --shell /usr/sbin/nologin shop
  install -d -o shop -g shop /data/log/shop /data/sessions
  install -d /etc/shop "/opt/shop/releases/$SHOP_VERSION"
  install -m 0755 "$build/bin/$arch/shop" "/opt/shop/releases/$SHOP_VERSION/shop"
  ln -sfn "/opt/shop/releases/$SHOP_VERSION" /opt/shop/current
  # Alternate builds that scenarios deploy as "the new version".
  for variant in "$build/bin/$arch"/shop-*; do
    name=${variant##*/shop-}
    install -D -m 0755 "$variant" "/usr/local/lib/shop-builds/$name/shop"
  done
  # The thumbnail sidecar. 1.5.0 is the alternate release a scenario deploys.
  install -d -o shop -g shop /data/uploads
  local v
  for v in 1.4.2:False 1.5.0:True; do
    local dir="/opt/shop-thumbs/releases/${v%%:*}"
    [[ ${v%%:*} == 1.5.0 ]] && dir=/usr/local/lib/shop-builds/thumbd-1.5.0
    install -d "$dir"
    sed "s/@VERSION@/${v%%:*}/; s/@SPILL@/${v##*:}/" "$files/thumbd.py" >"$dir/thumbd"
    chmod 0755 "$dir/thumbd"
  done
  ln -sfn /opt/shop-thumbs/releases/1.4.2 /opt/shop-thumbs/current
  [[ -f /etc/shop/shop.env ]] || install -m 0640 -g shop "$files/shop.env" /etc/shop/shop.env
  install -m 0644 "$files"/shop*.service /etc/systemd/system/
  install -m 0644 "$files/shop-maintenance.cron" /etc/cron.d/shop-maintenance
  install -m 0644 "$files/nginx-shop.conf" /etc/nginx/sites-available/shop
  ln -sfn /etc/nginx/sites-available/shop /etc/nginx/sites-enabled/shop
  rm -f /etc/nginx/sites-enabled/default
  systemctl daemon-reload
}

seed_database() {
  log "seed ($SEED_ORDERS orders; takes a few minutes)"
  local have
  have=$(mysql -N -B shop -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='shop' AND table_name='orders'")
  if [[ "$have" == 1 ]] && [[ $(mysql -N -B shop -e 'SELECT COUNT(*) FROM orders') -gt 0 ]]; then
    log "already seeded"
    return
  fi
  (
    set -a
    # shellcheck source=files/shop.env
    source /etc/shop/shop.env
    set +a
    SHOP_LOG_FILE='' /opt/shop/current/shop seed \
      --products "$SEED_PRODUCTS" --customers "$SEED_CUSTOMERS" --orders "$SEED_ORDERS"
  )
  mysql shop -e 'ANALYZE TABLE products, customers, orders'
}

fetch_tarball() { # url, member to extract, destination
  local tmp
  tmp=$(mktemp -d)
  curl -fsSL "$1" | tar -xz -C "$tmp"
  install -m 0755 "$(find "$tmp" -name "$2" -type f | head -1)" "$3"
  rm -rf "$tmp"
}

exporter_unit() { # name, user, exec
  sed -e "s|@NAME@|$1|" -e "s|@USER@|$2|" -e "s|@EXEC@|$3|" "$files/exporter.service.tmpl" \
    >"/etc/systemd/system/$1.service"
}

# Customers' wishlists live in Redis and never expire, as a real shop's
# long-lived Redis data would. They make Redis's snapshot about 30 MB, so a
# full root disk reliably stops Redis from saving.
seed_wishlists() {
  log "wishlists"
  systemctl start redis-server
  if [[ $(redis-cli --scan --pattern 'wishlist:*' --count 1000 | head -1) ]]; then
    log "already seeded"
    return
  fi
  python3 - "$SEED_CUSTOMERS" "$SEED_PRODUCTS" <<'PY' | redis-cli --pipe >/dev/null
import random, sys
customers, products = int(sys.argv[1]), int(sys.argv[2])
rng = random.Random(42)
out = sys.stdout.buffer
def cmd(*args):
    out.write(b"*%d\r\n" % len(args))
    for a in args:
        a = str(a).encode()
        out.write(b"$%d\r\n%s\r\n" % (len(a), a))
for c in rng.sample(range(1, customers + 1), customers // 2):
    items = []
    for _ in range(rng.randint(3, 25)):
        items += [rng.randint(1, products), 1700000000 + rng.randint(0, 60000000)]
    cmd("HSET", f"wishlist:{c}", *items)
PY
  redis-cli save >/dev/null
}

install_exporters() {
  log "exporters"
  id exporter >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin exporter
  local gh=https://github.com
  if [[ $mode == container ]]; then
    fetch_tarball() { :; } # binaries are in the image
  fi
  fetch_tarball "$gh/prometheus/node_exporter/releases/download/v$NODE_EXPORTER_VERSION/node_exporter-$NODE_EXPORTER_VERSION.linux-$arch.tar.gz" \
    node_exporter /usr/local/bin/node_exporter
  fetch_tarball "$gh/ncabatoff/process-exporter/releases/download/v$PROCESS_EXPORTER_VERSION/process-exporter-$PROCESS_EXPORTER_VERSION.linux-$arch.tar.gz" \
    process-exporter /usr/local/bin/process-exporter
  fetch_tarball "$gh/prometheus/mysqld_exporter/releases/download/v$MYSQLD_EXPORTER_VERSION/mysqld_exporter-$MYSQLD_EXPORTER_VERSION.linux-$arch.tar.gz" \
    mysqld_exporter /usr/local/bin/mysqld_exporter
  fetch_tarball "$gh/oliver006/redis_exporter/releases/download/v$REDIS_EXPORTER_VERSION/redis_exporter-v$REDIS_EXPORTER_VERSION.linux-$arch.tar.gz" \
    redis_exporter /usr/local/bin/redis_exporter

  install -d /etc/prometheus-exporters
  install -m 0644 "$files/process-exporter.yml" /etc/prometheus-exporters/process-exporter.yml
  install -m 0640 -g exporter /dev/stdin /etc/prometheus-exporters/mysqld.cnf <<'EOF'
[client]
user = exporter
password = exporter
socket = /run/mysqld/mysqld.sock
EOF
  exporter_unit node_exporter exporter "/usr/local/bin/node_exporter --web.listen-address=:9100 --collector.systemd --collector.processes"
  # process-exporter reads every process's /proc entries, so it runs as root.
  exporter_unit process-exporter root "/usr/local/bin/process-exporter --web.listen-address=:9256 -config.path /etc/prometheus-exporters/process-exporter.yml"
  exporter_unit mysqld_exporter exporter "/usr/local/bin/mysqld_exporter --web.listen-address=:9104 --config.my-cnf=/etc/prometheus-exporters/mysqld.cnf --collect.info_schema.innodb_metrics --collect.perf_schema.eventsstatements"
  exporter_unit redis_exporter exporter "/usr/local/bin/redis_exporter -web.listen-address=:9121 -redis.addr=redis://127.0.0.1:6379"
}

install_alloy() {
  log "grafana alloy $ALLOY_VERSION"
  if [[ $mode != container ]]; then
    local tmp
    tmp=$(mktemp -d)
    curl -fsSL -o "$tmp/alloy.zip" "https://github.com/grafana/alloy/releases/download/v$ALLOY_VERSION/alloy-linux-$arch.zip"
    unzip -q -o "$tmp/alloy.zip" -d "$tmp"
    install -m 0755 "$tmp/alloy-linux-$arch" /usr/local/bin/alloy
    rm -rf "$tmp"
  fi
  install -d /etc/alloy /var/lib/alloy
  install -m 0644 "$files/alloy.config" /etc/alloy/config.alloy
  cat >/etc/systemd/system/alloy.service <<'EOF'
[Unit]
Description=Grafana Alloy
After=network-online.target

[Service]
ExecStart=/usr/local/bin/alloy run --storage.path=/var/lib/alloy --server.http.listen-addr=127.0.0.1:12345 /etc/alloy/config.alloy
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
EOF
}

install_harness() {
  log "harness directories"
  install -d -m 0700 /var/lib/opsschool /var/lib/opsschool/state /opt/opsschool
}

enable_services() {
  log "services"
  sed -i 's/^ENABLED=.*/ENABLED="true"/' /etc/default/sysstat
  systemctl daemon-reload
  systemctl enable --now redis-server mysql cron sysstat \
    shop-payments shop shop-worker shop-thumbs nginx \
    node_exporter process-exporter mysqld_exporter redis_exporter alloy
  systemctl restart nginx
}

main() {
  install_packages
  install_mysql
  setup_data_volume
  configure_mysql
  setup_swap
  initrd_without_network
  configure_site_dns
  configure_service_segment
  configure_firewall
  install_shop
  configure_tls
  seed_database
  seed_wishlists
  install_exporters
  install_alloy
  install_harness
  enable_services
  log "done"
}

main "$@"
