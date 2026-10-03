#!/usr/bin/env bash
# Provisions the single-node scenario VM. Runs once, as root, inside the VM
# when the image is built. $1 is the build directory copied in by build.sh:
# it holds this script, files/, versions.env and the shop binaries.
#
# OPSSCHOOL_PROVISION=container adapts it for the container driver: MySQL
# comes from the distribution and the exporters and Alloy are already in
# the image.
set -euo pipefail

build=${1:?usage: provision.sh <build-dir>}
files="$build/files"
# shellcheck source=versions.env
source "$build/versions.env"
arch=$(dpkg --print-architecture) # amd64 or arm64
codename=$(. /etc/os-release && echo "$VERSION_CODENAME")
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
    strace lsof sysstat tcpdump dnsutils iproute2 iptables htop procps psmisc \
    net-tools ncat less vim-tiny
  # Tracing tools vary by distribution; install what exists.
  for pkg in bpftrace linux-perf; do
    apt-get install -y -q --no-install-recommends "$pkg" || echo "skipping $pkg"
  done
}

install_mysql() {
  if [[ $mode == container ]]; then
    log "mysql (distribution package)"
    apt-get install -y -q mysql-server
    return
  fi
  log "mysql ($MYSQL_SERIES)"
  # The -2023 file carries an expired copy of this key; -2025 extends it.
  curl -fsSL "https://repo.mysql.com/$MYSQL_KEY_FILE" | gpg --dearmor -o /usr/share/keyrings/mysql.gpg
  if ! gpg --show-keys --with-colons /usr/share/keyrings/mysql.gpg | grep -q "^fpr:::::::::$MYSQL_KEY_FPR:"; then
    echo "MySQL signing key does not have fingerprint $MYSQL_KEY_FPR; check https://repo.mysql.com" >&2
    exit 1
  fi
  echo "deb [signed-by=/usr/share/keyrings/mysql.gpg] http://repo.mysql.com/apt/debian $codename $MYSQL_SERIES" \
    >/etc/apt/sources.list.d/mysql.list
  apt-get update -q
  # Empty root password: root logs in over the socket with auth_socket.
  echo "mysql-community-server mysql-community-server/root-pass password " | debconf-set-selections
  echo "mysql-community-server mysql-community-server/re-root-pass password " | debconf-set-selections
  apt-get install -y -q mysql-server
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
    mkfs.ext4 -q -F -L shopdata /var/lib/data.img
    mkdir -p /data
    echo '/var/lib/data.img /data ext4 loop,defaults 0 2' >>/etc/fstab
  fi
  mountpoint -q /data || mount /data
}

configure_mysql() {
  log "mysql config"
  systemctl stop mysql
  if [[ ! -d /data/mysql ]]; then
    cp -a /var/lib/mysql /data/mysql
    chown -R mysql:mysql /data/mysql
  fi
  install -m 0644 "$files/mysql-shop.cnf" /etc/mysql/mysql.conf.d/zz-shop.cnf
  systemctl start mysql
  mysql <<'SQL'
CREATE DATABASE IF NOT EXISTS shop;
CREATE USER IF NOT EXISTS 'shop'@'localhost' IDENTIFIED BY 'shop';
GRANT ALL ON shop.* TO 'shop'@'localhost';
CREATE USER IF NOT EXISTS 'exporter'@'localhost' IDENTIFIED BY 'exporter' WITH MAX_USER_CONNECTIONS 3;
GRANT PROCESS, REPLICATION CLIENT, SELECT ON *.* TO 'exporter'@'localhost';
SQL
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
# dummy interface (svc0, 10.53.0.10) and forwards everything else upstream.
# systemd-resolved uses it for all lookups. networking/1.1 breaks this path.
configure_site_dns() {
  [[ $mode == container ]] && return # Docker owns resolv.conf; see decisions.md
  log "site dns"
  local upstream
  upstream=$(resolvectl dns eth0 | awk '{print $NF}')
  install -m 0644 "$files/svc0.netdev" "$files/svc0.network" /etc/systemd/network/
  networkctl reload
  install -d /etc/dnsmasq.d
  sed "s/@UPSTREAM@/${upstream:-192.168.5.3}/" "$files/dnsmasq-site.conf" >/etc/dnsmasq.d/site.conf
  apt-get install -y -q --no-install-recommends dnsmasq
  # dnsmasq only serves the site zone here. Keep Debian's helper from handing
  # it a resolv file and registering 127.0.0.1 with resolvconf: both log
  # warnings at every boot that look like DNS trouble.
  sed -i 's/^#IGNORE_RESOLVCONF=yes/IGNORE_RESOLVCONF=yes/; s/^#DNSMASQ_EXCEPT="lo"/DNSMASQ_EXCEPT="lo"/' /etc/default/dnsmasq
  install -d /etc/systemd/resolved.conf.d
  install -m 0644 "$files/resolved-site-dns.conf" /etc/systemd/resolved.conf.d/site-dns.conf
  systemctl restart systemd-resolved
  systemctl enable dnsmasq
  systemctl restart dnsmasq
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
  # Alloy pushes logs to telemetry.opsschool.internal. In the VM that is the
  # host; the container driver points it at the Loki container instead.
  if [[ $mode != container ]] && ! grep -q telemetry.opsschool.internal /etc/hosts; then
    echo "192.168.5.2 telemetry.opsschool.internal" >>/etc/hosts
  fi
  systemctl daemon-reload
  systemctl enable --now redis-server mysql cron sysstat \
    shop-payments shop shop-worker nginx \
    node_exporter process-exporter mysqld_exporter redis_exporter alloy
  systemctl restart nginx
}

main() {
  install_packages
  install_mysql
  setup_data_volume
  configure_mysql
  setup_swap
  configure_site_dns
  configure_firewall
  install_shop
  configure_tls
  seed_database
  install_exporters
  install_alloy
  install_harness
  enable_services
  log "done"
}

main "$@"
