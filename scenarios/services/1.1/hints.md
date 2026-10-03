nginx answers, so the proxy is fine. Is the service behind it running? Try `systemctl status shop`.
---
The service keeps restarting. Its own error message says why: `journalctl -u shop -n 50`.
---
Something in `/etc/shop/shop.env` changed. The change tool kept the previous version next to it; `diff` the two.
---
Fix the value in `shop.env` itself, then restart every service that reads it. `shop check-config` validates the file.
