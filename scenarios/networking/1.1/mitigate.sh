#!/usr/bin/env bash
# Reference mitigation: pin the payments name in /etc/hosts. Checkout works,
# but DNS is still broken for every other lookup.
set -euo pipefail
echo "127.0.0.1 payments.shop.internal" >>/etc/hosts
