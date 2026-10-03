Reproduce it from the machine: `curl -v https://api.shop.internal/health`. What does curl say about the certificate?
---
Look at what the server actually sends: `openssl s_client -connect 127.0.0.1:443 -servername api.shop.internal -showcerts </dev/null`. How many certificates are in the chain, and who issued each?
---
The internal CA lives in `/etc/ssl/shop-ca`, and `shop-cert-issue` issues certificates with the right chain. Compare what nginx serves with what it should serve.
---
Fixing one site's chain isn't the whole job. Check every certificate nginx serves, including when each one expires: `openssl x509 -noout -subject -enddate -in <file>`.
