#!/usr/bin/env bash
# Reference mitigation: delete the live rule. The persisted config still has
# it, so it comes back on the next boot.
set -euo pipefail
iptables -D INPUT -p tcp --dport 8080 -m comment --comment "app port: proxy only (SEC-2291)" -j "$OPSSCHOOL_VAR_ACTION"
