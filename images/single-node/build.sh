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
# A half-built base must not be mistaken for a good one: delete it unless
# the build reaches the end, where the ready marker is written. Set
# OPSSCHOOL_KEEP_FAILED=1 to keep it running for debugging; without the
# marker it is still never used as a base.
built=false
cleanup() {
  rm -rf "$build"
  if [[ $built != true ]]; then
    if [[ ${OPSSCHOOL_KEEP_FAILED:-} == 1 ]]; then
      echo "==> build failed; keeping $instance for debugging (limactl shell $instance)" >&2
      return
    fi
    echo "==> build failed; deleting $instance" >&2
    limactl delete -f "$instance" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT
limactl start "$instance"
limactl copy -r "$build" "$instance:/tmp/opsschool-build"
limactl shell "$instance" sudo bash /tmp/opsschool-build/provision.sh /tmp/opsschool-build
limactl shell "$instance" sudo bash /tmp/opsschool-build/smoke.sh
limactl shell "$instance" sudo rm -rf /tmp/opsschool-build
limactl stop "$instance"
# The marker holds the fingerprint of the files the image was built from
# (opsschool image build sets it), so sessions can tell when it's out of date.
printf '%s\n' "${OPSSCHOOL_IMAGE_FINGERPRINT:-}" >"$(limactl list --format '{{.Dir}}' "$instance")/opsschool-ready"
built=true
echo "==> $instance is built and stopped; sessions start from clones of it"
