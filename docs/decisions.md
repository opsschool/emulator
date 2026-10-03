# Decisions

Changes to [design.md](design.md) and judgment calls made while building.
Newest first.

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
  resolver, authoritative for `shop.internal`. systemd-resolved uses it via
  `/etc/systemd/resolved.conf.d/site-dns.conf`. The shop reaches payments as
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

`repo.mysql.com/RPM-GPG-KEY-mysql-2023` serves the release key with its
original expiry (2025-10-22), and Debian 13's `sqv` rejects it.
`RPM-GPG-KEY-mysql-2025` is the same key (fingerprint `BCA4 3417 … 785C`) with
an extended expiry. `versions.env` pins the file name and fingerprint, and
`provision.sh` checks the fingerprint.

## 2026-10-03: Curriculum links

The curriculum moved from `ops-school.readthedocs.io/en/latest/<chapter>.html`
(now a 404) to `https://www.opsschool.org/<chapter>.html`. Scenarios link
there.

## 2026-09-30: Container driver for development and CI

Lima needs hardware virtualization, which the environment this was built in
lacks and CI runners may lack. `--driver container` runs the scenario
machine as a privileged systemd container built from
`images/single-node/container/Dockerfile` and the same `provision.sh`
(`OPSSCHOOL_PROVISION=container`). Differences from the VM:

- Ubuntu 24.04 and its MySQL 8.0 package instead of Debian 13 and MySQL
  8.4, because the Debian and MySQL download hosts were unreachable here.
- It shares the host kernel: no kernel tuning, and a reboot is a container
  restart.
- Telemetry joins a Docker network (`opsschool`) and scrapes the machine
  directly; Loki gets the alias `telemetry.opsschool.internal`, which Alloy
  pushes to. In the VM that name points at the host (192.168.5.2).

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
