What do the shop's error logs say? `journalctl -u shop -p err -n 20`.
---
MySQL is refusing connections. Who is holding them? `mysql -e 'SHOW PROCESSLIST'` (root can still log in: MySQL keeps one connection for administrators).
---
Most connections are idle, but `SELECT * FROM information_schema.innodb_trx` shows they are inside transactions. Something opens a transaction and never finishes it.
---
When did this start? Look at what is deployed: `ls -l /opt/shop/current /opt/shop/releases` and `/var/log/shop-deploy.log`.
