#!/usr/bin/env bash
# Reference mitigation: pin the payments name in /etc/hosts. Checkout works,
# but DNS is still broken for every other lookup.
set -euo pipefail
echo "10.54.0.20 payments.shop.internal" >>/etc/hosts
