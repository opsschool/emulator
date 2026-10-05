#!/usr/bin/env bash
# Turns a clone of a Lima-built base into a disk any VM host can boot:
# removes Lima's agent, user and network config, resets cloud-init so the
# next boot configures the machine from its new host's data, and powers
# off. Run as root in the clone; `opsschool image build --driver kubevirt`
# runs it. The clone is thrown away after its disk is copied.
set -euo pipefail

lima_user=${1:?usage: vm-export.sh <lima user>}

systemctl disable --now lima-guestagent.service 2>/dev/null || true
rm -f /etc/systemd/system/lima-guestagent.service /usr/local/bin/lima-guestagent
rm -f /etc/sudoers.d/90-cloud-init-users /etc/netplan/50-cloud-init.yaml
# Lima's hosts entry; the new host's cloud-init writes its own.
sed -i '/host\.lima\.internal/d' /etc/hosts
truncate -s 0 /data/log/shop/*.log 2>/dev/null || true

# The rest runs after this shell's session is gone: the Lima user can't be
# deleted while it's logged in.
cat >/run/opsschool-export <<EOF
#!/bin/bash
sleep 2
pkill -KILL -u $lima_user || true
userdel -r $lima_user 2>/dev/null || true
rm -rf /home/$lima_user*
cloud-init clean --logs --seed --machine-id
journalctl --rotate --vacuum-time=1s >/dev/null 2>&1 || true
fstrim -av >/dev/null 2>&1 || true
systemctl poweroff
EOF
chmod +x /run/opsschool-export
systemd-run --unit opsschool-export --collect /run/opsschool-export
