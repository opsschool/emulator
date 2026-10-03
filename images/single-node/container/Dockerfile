# Container stand-in for the single-node VM, for development and CI where
# KVM is not available. See docs/decisions.md, "Container driver".
# Exporter and Alloy versions match versions.env.
FROM prom/node-exporter:v1.12.1 AS node
FROM prom/mysqld-exporter:v0.20.0 AS mysqld
FROM oliver006/redis_exporter:v1.92.1 AS redis
FROM ncabatoff/process-exporter:0.8.7 AS process
FROM grafana/alloy:v1.20.1 AS alloy

# Ubuntu 24.04, not 26.04 like the VM: systemd in 26.04 needs cgroup v2,
# and some Docker hosts (including WSL setups) still run cgroup v1.
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -q \
 && apt-get install -y -q --no-install-recommends systemd systemd-sysv dbus e2fsprogs mount \
 && apt-get clean && rm -rf /var/lib/apt/lists/*
COPY --from=node /bin/node_exporter /usr/local/bin/node_exporter
COPY --from=mysqld /bin/mysqld_exporter /usr/local/bin/mysqld_exporter
COPY --from=redis /redis_exporter /usr/local/bin/redis_exporter
COPY --from=process /bin/process-exporter /usr/local/bin/process-exporter
COPY --from=alloy /bin/alloy /usr/local/bin/alloy
# Units that make no sense in a container.
# The image's policy-rc.d stops package installs from starting services
# (nginx's default site needs IPv6, which containers lack). provision.sh
# starts everything itself.
RUN systemctl mask systemd-udevd.service systemd-modules-load.service sys-kernel-debug.mount \
      systemd-remount-fs.service getty.target console-getty.service
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
