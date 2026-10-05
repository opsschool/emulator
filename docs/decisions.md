# Decisions

Changes to [design.md](design.md) and judgment calls made while building.
Newest first.

## 2026-10-05: Hosted mode on Kubernetes

Most organisations will run Ops School as a web page with a shared
scoreboard, not as a CLI on each laptop. `opsschool serve` is that page
(the portal), and both it and the CLI are supported. See
[hosted.md](hosted.md) for running it. The design.md "Later phases" entry
imagined Firecracker microVMs and accounts; this is smaller:

- **One session is two pods.** The runner pod runs `opsschool _runner`,
  which plays `start` and then the session daemon, with Prometheus, Loki
  and Grafana as native sidecars on its loopback. The runner creates the
  machine pod (owned by the runner pod, so deleting one deletes both) and
  forwards the VM's usual ports (18080, 191xx) to it, as Lima does on a
  laptop. Checks, load and telemetry then run unchanged. One pod with the
  machine in it would be simpler, but a privileged container next to the
  telemetry and the session token would let the learner read or change
  their own grading. Learners are trusted, but there's no need to tempt
  them.
- **The machine is the container driver's image.** A real VM per session
  (KubeVirt, Firecracker) needs nested virtualization or bare metal, which
  most clusters don't have. Operators who want a kernel boundary can use
  a Kata runtime class (`--runtime-class`).
- **Reboots are soft reboots.** A pod's container that restarts loses its
  writable layer, so the reboot during verify would undo the learner's
  fix. `systemctl soft-reboot` restarts userspace and keeps the files,
  and `reboot.target` is linked to `soft-reboot.target` so typing `reboot`
  does the same. The kubelet bind-mounts `/etc/hosts`, `/etc/hostname` and
  `/etc/resolv.conf`; a soft reboot unmounts them like any file system and
  leaves the image's empty files, so their mount units get
  `DefaultDependencies=no`. Found when networking/2.1's verify failed
  every order after the reboot.
- **The portal proxies everything.** Browsers only reach the portal, which
  proxies `/s/<id>/` to the session's daemon and Grafana with the session's
  token. The daemon admits the token in place of its loopback-only check.
  The session page's URLs are all relative, so the same page works under
  `/s/<id>/` and on the CLI's 127.0.0.1:19999.
- **Identity** is a header set by a sign-in proxy (`--user-header`), or a
  name the learner types, kept in a cookie. Nothing checks the typed name;
  learners are trusted.
- **Results** go to a JSONL file on a volume, in the CLI's format, so the
  portal is one replica with the Recreate strategy. A database would allow
  more replicas, but one portal serves far more people than one cluster
  has sessions for.
- **One session per person**, and `--max-sessions` overall. A session ends
  itself ten minutes after its time limit, and the portal deletes any
  session older than `--max-age` in case a runner never ends.
- **Errors stay out of the learner's view.** When a runner fails, it
  writes the reason to its termination message. The portal logs it, and
  the learner sees only that something went wrong: the reason can name
  the fault, such as a break script's failing command.
- **No image fingerprint check.** The machine image comes from the
  registry, built with the same commit as the opsschool image by whoever
  deploys it; there's no checkout to compare against.
- **The quiz is CLI-only** for now. The session page has never had it.

## 2026-10-05: Container driver fixes for Docker Desktop

Tonight was the first time the container driver ran under Docker Desktop
on WSL2, and three things broke:

- systemd couldn't create its cgroups with `--cgroupns=host` ("Failed to
  create /docker/<id>/init.scope control group"). On cgroup v2 the driver
  now gives the machine a private cgroup namespace and no bind mount of
  `/sys/fs/cgroup`, as kind does.
- The CLI served its metrics on the Docker network's gateway address, which
  under Docker Desktop is inside Desktop's VM, so the listen failed. Under
  Docker Desktop the stack scrapes `host.docker.internal` instead, which
  reaches the CLI's loopback.
- `docker cp` can't write into tmpfs mounts, and the machine's `/run` is
  one, so copying checks to `/run/opsschool` failed. Copies now stream a
  tar through `docker exec`, as the Kubernetes driver does.

The Scenarios workflow only runs on pull requests, and recent changes were
merged without one, so it hadn't caught the last of these.

## 2026-10-05: Disk charts leave out bind-mounted files

Containers have `/etc/hostname`, `/etc/hosts` and `/etc/resolv.conf` bind
mounted from the host, and node_exporter reports each as a filesystem. The
disk charts and dashboard leave out mount points under `/etc/`. No real
data volume is mounted there.

## 2026-10-04: Sessions refuse an out-of-date image

A base image is built once, but it bakes in `images/<image>/` and the shop
built from `demoapp/`. A learner who pulls changes to either and keeps the
old image gets scenarios that fail in confusing ways. So `opsschool image
build` records a fingerprint of those files on the image (in the Lima ready
marker, or a Docker label), and `start` and `test` refuse an image whose
fingerprint doesn't match the checkout, with the command that rebuilds it.
Images built before this have no fingerprint and count as out of date.

- It's a hash of the files, not a version number someone has to remember to
  bump. The cost is that a comment change in `provision.sh` also asks for a
  rebuild.
- Tests and testdata in `demoapp/` are left out, and so are `go.mod` and
  `go.sum`: they change with the CLI's dependencies far more often than with
  the shop's. A dependency bump that matters to the shop needs a rebuild by
  hand.
- There's no flag to start anyway. Starting from a stale image is the
  confusing failure this is here to prevent.

## 2026-10-04: The VM's initrd doesn't bring up the network

Lima sessions took over two minutes to boot, and two of them were a
timeout. Each session is a clone of the base image with a new MAC address.
Ubuntu 26.04's initrd (dracut) brought the network card up as `enp0s4`, so
on first boot cloud-init couldn't rename it to `eth0` for the new MAC (the
link was busy), and `systemd-networkd-wait-online`, which netplan points at
`eth0`, waited its full 120 seconds. The image now builds its initrd
without dracut's network modules (including Ubuntu's `dyn-netconf`, which
depends on them). The root disk is local, so the initrd never needed them.
Boot went from 2 min 13 s to 15 s, and `opsschool start` from about three
minutes to under one before the baseline begins.

## 2026-10-04: The scoreboard shows the fastest fix

`opsschool list` shows each scenario's best score, as before, and the
learner's fastest time. The fastest run is the one with the quickest fix,
and the mitigation time shown is from that same run, not the quickest
mitigation of any run, so the two times always describe one attempt. Equal
fix times go to the earlier mitigation. When no run fixed the scenario, the
quickest mitigation is shown with no fix time. The best score and the
fastest run can be different attempts: a run that used the hint can still
be the fastest. Every result is already kept in `results.jsonl`, so this is
worked out when the list is shown, not stored separately.

## 2026-10-04: A session page in the browser

From the project owner, who chose the layout from three designs. While a
session runs, the daemon serves a page at `http://127.0.0.1:19999/`: the
incident, progress, both hints, Verify and End session on the right; on
the left a terminal on the scenario machine, or four dashboard charts, one
at a time. It follows the system's light or dark setting and has a switch;
the terminal is always dark. The font is Atkinson Hyperlegible Next, with
Atkinson Hyperlegible Mono in the terminal, chosen because similar letters
are easy to tell apart for readers whose first language isn't English.
Fonts and xterm.js are embedded in the binary, so the page works offline.
The CLI commands keep working alongside it.

Building the page into the local daemon first keeps the UI separate from
the harder parts of hosting (many VMs, accounts, isolation). A hosted
version can serve the same page.

- The terminal is xterm.js over a WebSocket to a pseudo-terminal running
  the same command as `opsschool shell`. Two pinned modules, because the
  standard library has neither: `github.com/coder/websocket` and
  `github.com/creack/pty`. Each tab is its own shell; a dropped
  connection, such as during a reboot, reconnects on the next keypress.
- The page draws its charts from fixed PromQL queries the daemon runs
  (`/api/charts`), so the page can't run arbitrary queries. Grafana is one
  click away for everything else.
- The page opens a root shell, so the daemon refuses requests whose Host
  isn't a loopback address (DNS rebinding) and browser requests from other
  origins (a site the learner visits). This covers the existing control
  API too. `/metrics` stays open for Prometheus on the Docker network.
- End session on the page records the result and tears the session down,
  as `opsschool stop` does.
- `opsschool start` tells the daemon when the healthy baseline ends, so the
  page can count down to the scenario.
- The terminal should feel like a Linux one: selecting text copies it, the
  middle button pastes it, and Ctrl+Shift+C copies. Browsers keep Ctrl+W,
  Ctrl+T and Ctrl+N for themselves, and a page can't take them in a normal
  tab. The terminal's full screen button uses the Keyboard Lock API (Chrome
  and Edge), which sends them to the shell. Outside full screen, the page
  asks before the tab closes while a shell is connected.

## 2026-10-04: /data is fully allocated; Redis holds customer wishlists

`/data` is an ext4 image on a loop device, backed by `/var/lib/data.img` on
the root disk. `mkfs.ext4` discarded the device, which punched holes in the
image file: only about 1 GB of its 6 GB was allocated. Once `/` was full,
writes to `/data` that needed new blocks failed, and MySQL couldn't create
`ibtmp1` on restart. The image is now formatted with `-E nodiscard` and without lazy
initialization (its zeroing punches holes too),
allocated in full with `fallocate`, and `fstrim.timer` is masked so it stays
that way. A smoke check guards it. `/data` now behaves like its own disk.

Redis held only short-lived cache, about 75 KB on disk, so a snapshot could
fit in the space ext4 leaves after a "full" disk and services/4.1 failed
only some of the time. The image now seeds customer wishlists
(`wishlist:<customer>` hashes, never expiring), which makes the snapshot
about 15 MB. The shop doesn't read them yet.

services/4.1 is now the Redis story: the root disk fills, Redis refuses
writes, checkout fails. MySQL is out of it.

## 2026-10-04: Hints: the curriculum first, free; then one paid hint

From the project owner. The first `opsschool hint` points at the scenario's
`curriculum` link and costs nothing. The second shows the scenario's one
hint, which costs `HintPenalty` (10 points) and points the way without
giving the answer. There are no more after that. `hints.md` holds exactly
one hint (more is a validation error), and `opsschool list` has a HINT
column saying whether the best result used it. Results record the free
hint as `docs_hint`, unscored.

Most linked curriculum chapters don't cover their scenario's topic yet;
the curriculum is improved alongside the scenarios.

## 2026-10-04: An edge load balancer, played by the load generator

Some faults drop connections before they reach the shop, so the shop's
metrics never see them (networking/3.1). A real site would see them at its
load balancer. The load generator now records every result as one would:
`edge_requests_total{route,code}` and `edge_request_duration_seconds`,
scraped as job `edge`, and an nginx-style access log in Loki
(`{job="edge"}`). Requests that got no response count as 504 (timeout) or
502 (refused, reset). The session daemon serves the metrics; `opsschool
test` serves them itself. The dashboard has an Edge row and an edge error
log panel. This replaces `opsschool_loadgen_requests_total`, whose name
gave away the harness.

`load.new_connections` sets the share of requests sent on a fresh
connection, as first visits from new customers would be. It defaults to 0
(reuse connections), so existing scenarios are unchanged.

## 2026-10-04: The harness works on a full disk

Scenario scripts used to be staged in `/tmp` and copied to
`/var/lib/opsschool` on the root disk, so once services/4.1 filled `/`
every check and fix script failed to copy. Copies now stage in `/dev/shm`
and scripts run from `/run/opsschool`, both in memory. Check state stays in
`/var/lib/opsschool/state`; it is written once, before the break.

## 2026-10-04: Image additions for the first L3–L4 scenarios

- **Payments on its own layer-2 segment (Lima only).** `shop-payments` runs
  in network namespace `pay1` (10.54.0.20, MAC 52:54:00:36:00:14), cabled
  to bridge `br-svc` (10.54.0.1, managed by systemd-networkd), and DNS
  points `payments.shop.internal` there. The shop now reaches payments as a
  real neighbor over ARP, which networking/3.2 needs. The container driver
  keeps payments on loopback. networking/1.1's `/etc/hosts` mitigation pins
  the new address.
- **A thumbnail sidecar.** `shop-thumbs` (a small Python service) makes
  thumbnails from photos in `/data/uploads/queue`. 1.4.2 runs; 1.5.0, with
  a scratch-file bug, sits in `/usr/local/lib/shop-builds` for services/4.1.
- **Tools:** `arping` and `conntrack`.
- `svc0` and `br-svc` are not required for network-online. Waiting for them
  logged an error-level timeout at every boot that looked like a network
  fault.

## 2026-10-04: L3–L4 ideas tried and shelved

- **Order ID overflow:** `orders.id` is already BIGINT, and the only
  mitigation is the fix, so the tiers would be the same.
- **Thread limit (`TasksMax`):** at this load neither the shop nor MySQL
  needs more than a thread or two beyond its idle count, because requests
  take about 2 ms. A limit low enough to bite at peak also breaks unrelated
  things at idle. Worth revisiting if the workload gains real concurrency.
- **Redis refusing writes after core dumps fill the disk:** systemd-coredump
  stops with less than one core's worth of space left, and Redis's snapshot
  fits in that. services/4.1 fills the disk with a scratch file instead,
  which writes until ENOSPC, and the incident became MySQL going down.

## 2026-10-04: Checkout takes a lock in Redis

Before this, the shop used Redis only as a cache, and every Redis error fell
back to MySQL, so no fault in Redis could hurt customers. Checkout now takes
a ten-second per-customer lock in Redis (`SET lock:checkout:<id> NX`), so a
double-clicked "Place order" can't charge twice. It fails closed: if Redis
can't take the lock, the order fails. That makes scenarios about Redis
possible (Redis refusing writes, evicting keys, a slow Redis). A request
that finds the lock held gets 409; the load generator's customers rarely
collide.

## 2026-10-04: The shop is Uncle Wally's Peanut Emporium

The README opens with a setting for new learners: they are the SRE at Uncle
Wally's Peanut Emporium, on call for its one server. The seeded catalog is
now peanut products instead of generic goods, so what learners see in the
database and API matches the story. Product names are not used by any check.

## 2026-10-04: Renamed from simulator to emulator

The project runs real software on a real machine, so the name is now the
Ops School emulator. The Go module is `github.com/opsschool/emulator`, and the
GitHub repo is renamed to match (GitHub redirects the old URL). The design spec's
original suggestion of `opsschool/simulator` is updated.

## 2026-10-04: Scenario IDs are `<category>/<level>.<n>`, and a printed page replaces the title

Scenario IDs and titles named the cause before the learner began
(`net-dns`, `perf-swap-thrash`, "Disk full", "Too many connections"), and
learners pick a scenario from `opsschool list`. Now a scenario lives in
`scenarios/<category>/<level>.<n>/`, for example `scenarios/linux/1.1/`, and
its ID is `linux/1.1`. The level comes from the directory, so scenario.yaml
has no `id`, `title` or `level`. `n` numbers scenarios within a level, and a
new scenario takes the next free number, so adding one never renumbers
another or reattaches recorded results. The category stays visible on
purpose. `list` shows only the ID and best result.

In place of a title, a scenario declares `alerts`: one-line alerts that are
printed as `[FIRING] ...` when the scenario begins, followed by the summary
as "what people are reporting". There are no real alert rules; the lines are
written to match what the break does. A scenario with no alerts
(services/2.1, where only API clients fail) says so and arrives as a report.

The old IDs map as follows. Results recorded under old IDs stay in
results.jsonl but no longer match a scenario.

| Old | New |
| --- | --- |
| linux-disk-full, linux-phantom-disk | linux/1.1, linux/2.1 |
| perf-cpu-cron, perf-swap-thrash | performance/1.1, performance/2.1 |
| net-dns, net-iptables-port | networking/1.1, networking/2.1 |
| db-too-many-conns, db-missing-index | databases/1.1, databases/2.1 |
| svc-crashloop-env, svc-tls-chain | services/1.1, services/2.1 |

The catalog in design.md uses the same scheme for the planned L3–L4
scenarios.

## 2026-10-03: The mitigated hold survives a verification

The mitigated tier needs its checks to keep passing for `mitigate_hold`.
Verify pauses that grading, because it restarts services and can reboot.
It also used to restart the hold from zero, so a learner who verified early
got mitigated credit late, or never, if they stopped soon after. Now the hold
carries on through a verification whose own mitigated checks pass at the
end; if they fail, it restarts. Mitigated is credited when the hold
completed, not at the next check after it.

## 2026-10-03: A healthy baseline before each scenario begins

`start` used to start telemetry, boot the machine and break it straight
away. The dashboards' first minutes then mixed the boot into every rate()
window. For example, perf-swap-thrash showed 99% CPU while its summary says
CPU looks low. There was also no normal traffic to compare the incident
against. Now `start` boots the machine, then starts telemetry, then runs
load for a two-minute healthy baseline before the break. It explains the
wait and counts down "Scenario begins in m:ss". The clock, grading, hints
and verify all start when the scenario begins. `opsschool test` uses the
same boot-then-telemetry order but skips the baseline.

## 2026-10-03: Verify shows learners check names, not check output

A failed `opsschool verify` printed each failing check's output, which often
names the cause ("no cron job cleans up /data/sessions at least hourly").
Learners could verify early to get the answer. Learners now see only the
names of the failing checks; the output goes to the session daemon log and
to `opsschool test`. Check names are now learner-visible text, so they state
the goal, not the cause, and four were renamed.

## 2026-10-03: Telemetry networking under Docker Desktop on Linux and WSL2

The stack used host networking whenever the CLI ran on Linux. Under Docker
Desktop, including its WSL2 integration, the "host" network is Docker
Desktop's own VM, so Prometheus never listened on the CLI's 127.0.0.1 and
`opsschool test` timed out waiting for it. The CLI now asks `docker info`
and uses host networking only with Docker Engine. Under Docker Desktop it
uses the macOS setup: published ports on 127.0.0.1, with scrapes going
through `host.docker.internal`. On WSL2 that name reaches the distro's
loopback, where Lima forwards the VM's ports. Alloy reaches Loki through
Lima's host address and the published port.

## 2026-10-03: Journal priorities for the shop's logs

The shop logged JSON to stdout with no priority, so journald recorded every
line, errors included, at priority 6 and `journalctl -p err` showed nothing.
Stdout lines now start with a syslog prefix (`<3>` for errors, `<4>` for
warnings), which journald strips and records as the priority. The log file
stays plain JSON.

## 2026-10-03: M4 is nine scenarios, not eleven

The catalog has twelve L1–L2 scenarios, but `dist-bad-healthcheck` and
`dist-clock-skew` need the multi-node image. M4 is the nine single-node
L1–L2 scenarios besides `linux-disk-full`; those two move to M5 with the
rest of the distributed category.

## 2026-10-03: Scenario variants left out

- `db-too-many-conns` implements the connection leak only. The binlog
  disk-full variant overlaps `linux-disk-full`.
- `linux-phantom-disk` implements both of its variants, picked by seed.

## 2026-10-03: Image additions for M4

- Swap: a 2 GB swap file, as most general-purpose hosts have.
  `perf-swap-thrash` needs it. The container driver has no swap of its own
  and no memory limit, so that scenario doesn't thrash there.
- The worker's render buffer is 256 MB (4 workers, 1 GB). With 32 MB buffers,
  even 128 workers did not thrash: only the few goroutines handling an order
  touch their buffer, and the active set fit in RAM.
- Site DNS: dnsmasq on a dummy interface `svc0` (10.53.0.10) is the site
  resolver, authoritative for `shop.internal`. systemd-resolved sends
  `shop.internal` lookups to it via
  `/etc/systemd/resolved.conf.d/site-dns.conf` and everything else to the
  network's resolver. (It first sent all lookups through dnsmasq, but on
  Ubuntu 26.04 Lima's resolver refuses some of what dnsmasq forwards, and
  dnsmasq logs a warning at every boot that looks like the DNS fault.)
  dnsmasq forwards anything else asked of it to resolved's stub, and
  `local=/shop.internal/` keeps it from forwarding `shop.internal` AAAA
  queries, which would loop back to it. The shop reaches payments as
  `payments.shop.internal:8081`. This replaces "Payments by IP for now". The
  container driver can't do this, because Docker owns `/etc/resolv.conf`; it
  adds the `shop.internal` names as host entries instead, and `net-dns` only
  works under Lima.
- Firewall: `iptables-persistent`, with base rules in
  `/etc/iptables/rules.v4` (MySQL and Redis local-only).
- TLS: an internal root and intermediate CA in `/etc/ssl/shop-ca`, with the
  root in the system trust store. nginx serves `api.shop.internal` and
  `partners.shop.internal` on 443. `shop-cert-issue <host>` issues a
  certificate with its full chain.
- `shop check-config` validates the configuration in the environment and
  exits, like `nginx -t`.

## 2026-10-03: perf-cpu-cron runs its job at nice -10

A per-minute job running two `gzip -9` processes per core at normal priority
barely moved the shop's latency: p99 went from 25 ms to about 30 ms, because
the scheduler serves short wakeups quickly. The job now runs at `nice -n -10`
("so the feed is never late"), which takes p99 to 100–180 ms and p50 from
2 ms to 20 ms. Healthy p99 at peak load is about 25 ms, so the checks use
80 ms.

## 2026-10-03: The MySQL signing key moved

Superseded on merge: main had already moved the VM to Ubuntu 26.04 and its
own MySQL 8.4 package (see 2026-10-01), so the MySQL apt repository and this
key pin are gone. Kept for the record:

`repo.mysql.com/RPM-GPG-KEY-mysql-2023` serves the release key with its
original expiry (2025-10-22), and Debian 13's `sqv` rejects it.
`RPM-GPG-KEY-mysql-2025` is the same key (fingerprint `BCA4 3417 … 785C`) with
an extended expiry. `versions.env` pins the file name and fingerprint, and
`provision.sh` checks the fingerprint.

## 2026-10-03: Curriculum links

The curriculum moved from `ops-school.readthedocs.io/en/latest/<chapter>.html`
(now a 404) to `https://www.opsschool.org/<chapter>.html`. Scenarios link
there.
## 2026-10-01: Ubuntu 26.04 for the VM and the container

The first Lima build failed because the signing key of MySQL's apt
repository expired on 2025-10-22. Ubuntu 26.04 LTS ships MySQL 8.4 in its
own archive, so the VM now uses the Ubuntu 26.04 cloud image and installs
MySQL from the distribution, with no third-party apt repository. The
project owner had already approved Ubuntu.

The container driver stays on Ubuntu 24.04 (MySQL 8.0): systemd in 26.04
requires cgroup v2 and will not boot on Docker hosts that still use cgroup
v1, which includes the environment this was built in and some WSL setups.

Ubuntu's AppArmor profile for mysqld only allows `/var/lib/mysql`, so
provisioning adds an AppArmor alias for `/data/mysql`.

Alloy now pushes logs to `host.lima.internal` in both modes: Lima resolves
it to the host, and the container driver gives the Loki container that
alias. Provisioning no longer edits `/etc/hosts`.

## 2026-09-30: Container driver for development and CI

Lima needs hardware virtualization, which the environment this was built in
lacks and CI runners may lack. `--driver container` runs the scenario
machine as a privileged systemd container built from
`images/single-node/container/Dockerfile` and the same `provision.sh`
(`OPSSCHOOL_PROVISION=container`). Differences from the VM:

- Ubuntu 24.04 and MySQL 8.0, while the VM runs Ubuntu 26.04 and MySQL 8.4
  (see 2026-10-01).
- It shares the host kernel: no kernel tuning, and a reboot is a container
  restart.
- Telemetry joins a Docker network (`opsschool`) and scrapes the machine
  directly; Loki gets the alias `host.lima.internal`, which Alloy pushes
  to, as it does in the VM.

Lima stays the default whenever `limactl` is installed. The project owner
approved Ubuntu for the container image and the checkout-session change
that makes a full disk fail orders promptly.

## 2026-09-30: Sessions clone a stopped base machine

`opsschool image build <image>` provisions a base machine once (Lima
instance `opsschool-<image>`, or Docker image `opsschool/<image>:base`).
Each session starts from a clone, so `start` never reinstalls packages and
every session begins clean. `limactl clone` needs Lima 1.1 or later.

## 2026-09-30: A background daemon runs the session

`opsschool start` sets up, breaks, then starts `opsschool _daemon`, which
runs until `stop`. It drives load, grades `mitigated` every 5s, serves the
CLI's metrics on 127.0.0.1:19999, and answers the other commands over a
small HTTP API on the same port. State is in `~/.opsschool/session/`.

## 2026-09-30: What "fixed" means

`opsschool verify` restarts the listed units, reboots if required, replays
peak load for `load_replay`, waits 15s, then passes `fixed` only if the
fixed checks, the mitigated checks (without the hold) and the preserve
checks all pass. A passing verify also marks `mitigated` as passed, since
the service is healthy. A failed preserve check records data loss.

## 2026-09-30: "Checks must fail" means the tier fails

The spec says all `mitigated` and `fixed` checks must fail after the break.
Some checks, such as a health endpoint, legitimately pass during an
incident. `opsschool test` requires each tier to fail as a whole (at least
one check failing), which is what grading depends on.

## 2026-09-30: Payments by IP for now

The shop calls its payments stand-in at `127.0.0.1:8081`. The `net-dns`
scenario (M4) will need a hostname and a local resolver; that is left for
when the scenario is written. Superseded on 2026-10-03: see "Image additions
for M4".

## 2026-09-30: Download checksums

The image pins exporter, Alloy and MySQL versions but does not verify
checksums, because the release hosts were unreachable when this was
written. Add SHA-256 checks to `provision.sh` when building with network
access.

## 2026-09-30: Go 1.27

`go.mod` pins Go 1.27 with toolchain 1.27.1, the current release.
`GOTOOLCHAIN=auto` downloads it if the local Go is older.

## 2026-09-30: Data volume at /data

The single-node image puts the MySQL data directory and the shop's logs on a
separate filesystem mounted at `/data`, as many production hosts do. Filling
it breaks database writes without filling the root filesystem, so SSH, the
harness and the exporters keep working. `linux-disk-full` checks
`mountpoint="/data"` instead of `/var`.

## 2026-09-30: Preserve checks record a baseline

Preserve checks (for example "no orders lost") need a value from before the
break to compare against. The harness runs every `preserve` script once with
`OPSSCHOOL_PHASE=baseline` before `break.sh`, then with
`OPSSCHOOL_PHASE=check` during fix verification. Scripts keep state in
`$OPSSCHOOL_STATE_DIR`, a directory the harness owns in the VM.

## 2026-09-30: Check templates

Check URLs, PromQL expressions and expected bodies can use `{{vm}}` (the
shop's address through nginx), `{{vm_admin}}` (the app's admin port, for
`/metrics` and pprof), `{{prometheus}}` and `{{var.<name>}}` for randomized
variables. `opsschool validate` rejects unknown keys.

## 2026-09-30: Scenario directory name equals its ID

The spec's lint rule says the ID matches the directory name, but its examples
used `scenarios/networking/mtu-blackhole/` for `net-mtu-blackhole` and
`scenarios/linux/disk-full` for `linux-disk-full`. The rule wins: scenarios
live at `scenarios/<category>/<id>/`, and the validator also checks the
category directory.

## 2026-09-30: Extra scenario.yaml fields

- `target_time`: time under which passing `fixed` earns the time bonus. The
  spec's scoring mentions a target time but the schema had no field for it.
- `load`: `profile: steady | peak`, or a custom `schedule` of `{rps, duration}`
  steps. The spec allowed a per-scenario load profile without saying where.
- `mitigate_hold` defaults to 60s and `time_limit` to 45m.

## 2026-09-30: Lint details

- `break.sh`, `mitigate.sh`, `solve.sh` and check scripts must have a bash
  shebang and `set -euo pipefail`.
- Management-channel rules in `break.sh`: no `eth0`, no firewall policy
  changes or full flushes, nothing touching SSH, the exporters, Alloy or
  harness paths. A line can opt out with `# lint:allow <rule>`.
- Missing `hints.md` or `SOLUTION.md` is a warning, not an error.

## 2026-09-30: Quiz answers

In `questions.yaml`, choice answers are 0-based indexes, as in the spec's
example. Learners see and type 1-based choice numbers.

## 2026-09-30: Learners are trusted

Users are employees who are trusted to be honest. There is no certification
or identity verification, and grading does not need to resist tampering. The
spec's preference for PromQL and HTTP checks over in-VM scripts was about
tampering, so it is dropped: use whichever check type is simplest. Telemetry
and grading still run on the host, because that keeps them working when the
VM runs out of memory or disk.

## 2026-09-30: Two tiers, and seniority from level

Decided with the project owner. The spec had three tiers: `mitigated`
(junior), `fixed` (intermediate) and `root_cause` (senior, a quiz). A quiz
after the fact is a weak test of seniority: on an L1 scenario anyone who fixed
it knows the answer. Now:

- Tiers are `mitigated` and `fixed`.
- Seniority comes from level and tier together: junior is `mitigated` on
  L1–L2, intermediate is `fixed` on L1–L2 and `mitigated` on L3–L4, senior is
  `fixed` on L3–L4.
- `questions.yaml` is an optional quiz (`opsschool quiz`), a learning check
  that does not affect the score.

## 2026-09-30: License

Apache 2.0 for the code, confirmed by the project owner. The curriculum
uses CC BY 3.0, which suits prose but not software.
