#!/usr/bin/env bash
# Builds the single-node base VM with Lima. Run from the repo root on the
# host: images/single-node/build.sh. Needs limactl and Go.
set -euo pipefail

instance=${OPSSCHOOL_BASE_INSTANCE:-opsschool-single-node}
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
build=$(mktemp -d)
trap 'rm -rf "$build"' EXIT

# shellcheck source=versions.env
source "$here/versions.env"

case $(uname -m) in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac

echo "==> building shop $SHOP_VERSION for linux/$arch"
mkdir -p "$build/bin/$arch"
ldflags="-s -w -X main.Version=$SHOP_VERSION"
(cd "$root" && CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags "$ldflags" -o "$build/bin/$arch/shop" ./demoapp/cmd/shop)
# Alternate builds are versioned as the next release, the way a bad deploy
# would be.
for tag in faultconnleak faultmemleak faultidletx faultnobackoff; do
  (cd "$root" && CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -tags "$tag" \
    -ldflags "-s -w -X main.Version=2.4.0" -o "$build/bin/$arch/shop-${tag#fault}" ./demoapp/cmd/shop)
done
cp -r "$here/files" "$here/versions.env" "$here/provision.sh" "$here/smoke.sh" "$build/"

if limactl list -q | grep -qx "$instance"; then
  echo "==> deleting existing $instance"
  limactl delete -f "$instance"
fi
echo "==> creating $instance"
limactl create --tty=false --name "$instance" "$here/lima.yaml"
limactl start "$instance"
limactl copy -r "$build" "$instance:/tmp/opsschool-build"
limactl shell "$instance" sudo bash /tmp/opsschool-build/provision.sh /tmp/opsschool-build
limactl shell "$instance" sudo bash /tmp/opsschool-build/smoke.sh
limactl shell "$instance" sudo rm -rf /tmp/opsschool-build
limactl stop "$instance"
echo "==> $instance is built and stopped; sessions start from clones of it"
