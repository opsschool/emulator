#!/usr/bin/env bash
# Provisions the single-node scenario VM. Runs once, as root, inside the VM
# when the image is built. $1 is the build directory copied in by build.sh:
# it holds this script, files/, versions.env and the shop binaries.
set -euo pipefail

build=${1:?usage: provision.sh <build-dir>}
files="$build/files"
# shellcheck source=versions.env
source "$build/versions.env"
arch=$(dpkg --print-architecture) # amd64 or arm64
codename=$(. /etc/os-release && echo "$VERSION_CODENAME")
export DEBIAN_FRONTEND=noninteractive

log() { echo "==> $*"; }

install_packages() {
  log "packages"
  apt-get update -q
  apt-get install -y -q --no-install-recommends \
    ca-certificates curl gnupg unzip jq \
    nginx redis-server cron logrotate \
    strace lsof sysstat tcpdump dnsutils iproute2 iptables htop procps psmisc \
    net-tools ncat less vim-tiny bpftrace linux-perf
}

install_mysql() {
  log "mysql ($MYSQL_SERIES)"
  curl -fsSL https://repo.mysql.com/RPM-GPG-KEY-mysql-2023 | gpg --dearmor -o /usr/share/keyrings/mysql.gpg
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
    fallocate -l 6G /var/lib/data.img
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
  local tmp
  tmp=$(mktemp -d)
  curl -fsSL -o "$tmp/alloy.zip" "https://github.com/grafana/alloy/releases/download/v$ALLOY_VERSION/alloy-linux-$arch.zip"
  unzip -q -o "$tmp/alloy.zip" -d "$tmp"
  install -m 0755 "$tmp/alloy-linux-$arch" /usr/local/bin/alloy
  rm -rf "$tmp"
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
    shop-payments shop shop-worker nginx \
    node_exporter process-exporter mysqld_exporter redis_exporter alloy
  systemctl restart nginx
}

main() {
  install_packages
  install_mysql
  setup_data_volume
  configure_mysql
  install_shop
  seed_database
  install_exporters
  install_alloy
  install_harness
  enable_services
  log "done"
}

main "$@"
