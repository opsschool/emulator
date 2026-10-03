# Certificate errors after renewal

## What happened

The `api.shop.internal` certificate was renewed by hand last night and
installed without the intermediate CA certificate. A TLS server must send
its leaf certificate and every intermediate up to (not including) the root.
Clients trust only the root, so a client that receives just the leaf cannot
build a path to it:

    curl: (60) SSL certificate problem: unable to get local issuer certificate

Browsers mostly kept working, because they cache intermediates they have
seen before or fetch them using the certificate's AIA extension. The mobile
app and API clients don't. `openssl s_client -showcerts` shows a single
certificate where there should be two.

The `partners.shop.internal` certificate, served from the same nginx, was
issued a year ago and expires in a few days. Nothing was watching it.

## Mitigate

Serve the API certificate followed by the intermediate:

    cat api.shop.internal.crt /etc/ssl/shop-ca/intermediate.crt > fullchain.crt

point `ssl_certificate` at it (or replace the file) and reload nginx.

## Fix

Fix the API chain and renew the partners certificate before it expires.
`shop-cert-issue <hostname>` issues a fresh certificate with the full chain.
Then make sure the next expiry can't sneak up: monitor certificate expiry
for every name nginx serves.

## Curriculum

- [Security 201](https://www.opsschool.org/security_201.html)
- [HTTP 201](https://www.opsschool.org/http_201.html)
