The shop keeps restarting, and its own log says why: `journalctl -u shop -n 50`. Something in `/etc/shop/shop.env` changed recently; the tool that changed it kept the previous version nearby.
