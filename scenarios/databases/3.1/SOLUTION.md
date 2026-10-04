# AppArmor denial

## What happened

A tuning change (DB-212) set MySQL's `tmpdir` to a new directory on the data
volume, with the right owner and mode, and restarted MySQL. MySQL failed to
start: its error log says `Unable to create temporary file inside
"/data/mysql-tmp"; errno: 13`, permission denied.

Unix permissions allow it. The denial comes from AppArmor: Ubuntu confines
`mysqld` with a profile that lists the paths it may use, and the new
directory isn't one of them. The kernel log has the evidence, a line like
`apparmor="DENIED" operation="mknod" profile="/usr/sbin/mysqld"
name="/data/mysql-tmp/..."`.

## Mitigate

Get MySQL running: revert the tuning change, or take the profile out of the
way (switch it to complain mode with
`apparmor_parser -C /etc/apparmor.d/usr.sbin.mysqld`, or unload it with
`apparmor_parser -R`). Then restart MySQL and the shop.

## Fix

Keep the profile enforced and make the change work with it. Add the
directory to the profile's local additions, which survive package
upgrades, and reload the profile:

```
echo '/data/mysql-tmp/ rw,' >>/etc/apparmor.d/local/usr.sbin.mysqld
echo '/data/mysql-tmp/** rwk,' >>/etc/apparmor.d/local/usr.sbin.mysqld
apparmor_parser -r /etc/apparmor.d/usr.sbin.mysqld
systemctl restart mysql
```

Reverting the tuning change is also a fix. Leaving MySQL unconfined is not.

## Curriculum

- [Security 201: AppArmor](https://www.opsschool.org/security_201.html#apparmor)
