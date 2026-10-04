Find out exactly when checkout broke again, and what happened on the server at that moment. `journalctl --since` around that time.
---
Something rewrote the shop's configuration and restarted the shop. Who? Look at the journal entries just before the restart, and at `systemctl list-timers`.
---
The configuration comes from somewhere. Read the script the timer runs, and follow it to its source.
---
The source is a git repository. `git log -p` shows who changed what, and when.
