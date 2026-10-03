#!/usr/bin/env bash
# The API certificate was renewed by hand and installed without its
# intermediate, so only clients that already have the intermediate cached
# (browsers, mostly) can build a chain to the trusted root. Separately, the
# partners certificate was issued a year ago and is about to expire.
set -euo pipefail

tls=/etc/nginx/tls
ca=/etc/ssl/shop-ca

# Renew api.shop.internal, then install only the leaf.
/usr/local/sbin/shop-cert-issue api.shop.internal >/dev/null
openssl x509 -in "$tls/api.shop.internal.crt" -out "$tls/api.shop.internal.crt.new"
mv "$tls/api.shop.internal.crt.new" "$tls/api.shop.internal.crt"

# Reissue partners.shop.internal as it would look a year after issue.
days=$OPSSCHOOL_VAR_DAYS_LEFT
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
touch "$tmp/index.txt"
echo 1000 >"$tmp/serial"
cat >"$tmp/ca.cnf" <<CNF
[ca]
default_ca = issuing
[issuing]
database = $tmp/index.txt
serial = $tmp/serial
new_certs_dir = $tmp
certificate = $ca/intermediate.crt
private_key = $ca/intermediate.key
default_md = sha256
policy = any
unique_subject = no
[any]
commonName = supplied
[server]
subjectAltName = DNS:partners.shop.internal
extendedKeyUsage = serverAuth
keyUsage = critical,digitalSignature,keyEncipherment
CNF
openssl req -new -key "$tls/partners.shop.internal.key" -subj /CN=partners.shop.internal -out "$tmp/req.csr"
openssl ca -batch -notext -config "$tmp/ca.cnf" -extensions server -in "$tmp/req.csr" -out "$tmp/leaf.crt" \
  -startdate "$(date -u -d "-$((397 - days)) days" +%Y%m%d%H%M%SZ)" \
  -enddate "$(date -u -d "+$days days" +%Y%m%d%H%M%SZ)" 2>/dev/null
cat "$tmp/leaf.crt" "$ca/intermediate.crt" >"$tls/partners.shop.internal.crt"
systemctl reload nginx.service
