# Ops School Emulator: Design and Build Spec

This is the build spec for the Ops School emulator. It is written for Claude Code (or any engineer) to implement from. Changes made after the original spec are recorded in [decisions.md](decisions.md). The curriculum it supports lives at <https://ops-school.readthedocs.io>.

## Instructions for Claude Code

1. Confirm with the user: repo name (suggested `opsschool/emulator`), visibility (suggested public), and license. Then create the repo in the `opsschool` GitHub org with `gh repo create`.
2. Commit this file as `docs/design.md`.
3. Write a short `CLAUDE.md` at the repo root with the conventions from the "Conventions" section and a pointer to `docs/design.md`.
4. Work through the milestones in order. Each milestone has acceptance criteria. Stop for user review after M0 and after M3.
5. When something in this spec is ambiguous or turns out to be wrong in practice, pick the simplest option that satisfies the acceptance criteria, record the decision in `docs/decisions.md`, and mention it to the user.

## Goal

Learners start an emulated production environment in a broken state, debug it with real tools, and get graded as they go. The first audience is people learning to be SREs. The second is software engineers learning to run their own services.

A learner runs something like:

```
opsschool start linux/1.1 --user jdoe
```

They get a shell on a VM, a Grafana URL with live dashboards, and notifications as they pass each grading tier.

## Decisions already made

| Topic | Decision |
| --- | --- |
| Delivery order | Local-first. A CLI that runs on a laptop. Hosted mode comes later. |
| Identity | Self-reported username (`--user`). No verification in this phase. |
| Certification | None. Users are trusted to be honest. Results are recorded per username for progress tracking and fun. |
| Database | MySQL 8.4 LTS |
| Demo app language | Go |
| Telemetry | Prometheus, Loki and Grafana on the host, dashboards refresh every 5s |
| Contribution model | One directory per scenario, verified end to end in CI |

## Concepts

**Level (L1–L4)** is the difficulty of a scenario. L1 is a single obvious fault. L4 is several interacting faults, often across nodes.

**Tier** is how far the learner got within a scenario. Every scenario is graded on two tiers:

| Tier | Passes when |
| --- | --- |
| `mitigated` | The service passes its health probe continuously for 60s (configurable per scenario). |
| `fixed` | The cause is removed and the service survives a restart, a reboot (if the scenario requires it) and a load replay. |

**Seniority** comes from combining the two. Tiers are independent of level, so a junior can pass `mitigated` on an L4 scenario and gets credit for it.

| Seniority | Expected result |
| --- | --- |
| Junior | `mitigated` on L1–L2 |
| Intermediate | `fixed` on L1–L2, `mitigated` on L3–L4 |
| Senior | `fixed` on L3–L4: several interacting faults with harder-to-diagnose contributing factors |

**Quiz** is an optional set of questions about what happened. It is a learning check, not a tier, and does not affect the score.

## Architecture

```
Host (learner's laptop)
├── opsschool CLI (Go)
│   ├── scenario loader + validator
│   ├── VM driver (Lima)
│   ├── check engine (PromQL, HTTP probes, scripts)
│   └── results store (~/.opsschool)
├── Telemetry stack (Docker Compose)
│   ├── Prometheus  (scrape interval 5s)
│   ├── Loki
│   └── Grafana     (provisioned dashboards, refresh 5s)
└── Load generator (Go, runs on host)

Scenario VM (Lima)
├── Demo stack: nginx -> Go app -> MySQL 8.4 + Redis, worker, cron
├── Exporters: node_exporter, process-exporter, mysqld_exporter,
│              redis_exporter, app /metrics
├── Grafana Alloy: ships journald + app logs to Loki
└── Scenario break script (applied at start)
```

The telemetry stack runs on the host, not in the VM, so dashboards and grading keep working when the VM runs out of memory or disk.

### Management channel

Metrics, logs, harness commands and check scripts use a management channel that break scripts are not allowed to touch. With Lima this is the default network interface and its port forwards. Scenarios that break networking must operate on the service-facing interface or on firewall rules scoped to service ports. The scenario linter enforces this (see "CI and linting").

If Lima's networking makes a clean separation impractical, choose another approach that keeps telemetry and grading working during networking scenarios, and record it in `docs/decisions.md`.

## Repository layout

```
cmd/opsschool/          CLI entry point
internal/scenario/      spec types, loader, validator, templating
internal/vm/            VM driver interface + Lima implementation
internal/checks/        check engine: promql, http, script
internal/results/       results store and scoring
internal/telemetry/     compose lifecycle, dashboard assembly
internal/loadgen/       load generator
demoapp/                the Go demo service ("shop")
images/single-node/     Lima template + provisioning for the single-node image
images/multi-node/      (M5) multi-node image
telemetry/              docker-compose.yml, Prometheus/Loki/Grafana config
telemetry/dashboards/   base dashboard JSON
scenarios/<category>/<level>.<n>/   e.g. scenarios/linux/1.1/
docs/design.md          this file
docs/decisions.md
docs/writing-scenarios.md
CONTRIBUTING.md
.github/workflows/
```

## CLI

| Command | Behavior |
| --- | --- |
| `opsschool list` | List scenario IDs, the learner's best score, and their fastest fix with that run's mitigation time. |
| `opsschool start <id> --user <name>` | Boot the VM, start the telemetry stack, apply the break, start load. Print the session page, shell command and Grafana URL. Start the timer. |
| `opsschool shell` | Open a shell in the running scenario VM. |
| `opsschool status` | Show each tier's state, elapsed time and whether the hint was used. |
| `opsschool hint` | First call: point to the curriculum chapter, free. Second call: reveal the scenario's one hint, recorded for scoring. |
| `opsschool verify` | Learner claims a fix. Run fix verification: restart the service, reboot if the scenario requires it, replay load, then evaluate the `fixed` checks. |
| `opsschool quiz` | Answer the scenario's optional quiz questions. |
| `opsschool stop` | Tear down the VM and telemetry stack. Write the final result. |
| `opsschool validate <path>` | Validate a scenario directory (schema and lint). |
| `opsschool test <path>` | Run the full CI verification for one scenario locally. |

While a session runs, the daemon also serves a session page at `http://127.0.0.1:19999/` with the same actions: the incident, progress, hints, Verify and End session, a terminal on the scenario machine, and the key dashboard charts. See the decision "A session page in the browser".

Only one scenario runs at a time in the MVP.

The `mitigated` tier is evaluated continuously in the background while a scenario runs. `fixed` is evaluated only on `verify`, because fix verification is disruptive.

When a tier passes, the CLI prints a message, sends a desktop notification if available, and sets the tier's metric (see "Telemetry").

## Scenario spec

Each scenario is one directory:

```
scenarios/networking/4.1/
  scenario.yaml     metadata, image, randomized vars
  break.sh          applies the fault; runs as root in the VM
  checks.yaml       checks per tier
  mitigate.sh       reference mitigation (CI only, never shown to learners)
  solve.sh          reference fix (CI only)
  questions.yaml    optional quiz questions
  hints.md          one hint, shown after the free curriculum link
  dashboard.json    optional extra Grafana panels
  SOLUTION.md       writeup, links to the curriculum chapter
```

### scenario.yaml

```yaml
category: linux            # linux | performance | networking | databases | services | distributed
image: single-node         # single-node | multi-node
curriculum: https://ops-school.readthedocs.io/   # link the specific chapter
alerts:                    # optional; shown as [FIRING] lines when the scenario begins
  - "ShopOrderErrors: more than 5% of POST /orders requests are failing (5xx)"
summary: >
  Orders are failing. Customers report errors at checkout.
randomize:
  log_name:
    choices: [debug.log, trace.log, app-verbose.log]
  fill_size_mb:
    range: [800, 1200]
fix_verification:
  restart: [shop.service]
  reboot: false
  load_replay: 120s
mitigate_hold: 60s
time_limit: 45m
```

Rules:

- `randomize` values are chosen once per session from a seed and passed to every script as environment variables named `OPSSCHOOL_VAR_<NAME>` (uppercased).
- The session seed, username and scenario ID are passed as `OPSSCHOOL_SEED`, `OPSSCHOOL_USER` and `OPSSCHOOL_SCENARIO`.
- A scenario's ID is `<category>/<level>.<n>`, from its directory, for example `linux/1.1` or `databases/2.3`. The level (1 to 4) comes from the directory, not from scenario.yaml; `n` numbers the scenarios at that level from 1, and a new scenario takes the next free number. The ID says nothing else about the scenario, and scenarios have no title.
- `alerts` and `summary` are the only text shown to the learner at start. Each alert is one line, printed as `[FIRING] <line>`, like a page from the alerting system; leave it out when nothing would fire and the incident arrives as a report. The `summary` is what people are reporting. Both describe symptoms, never the cause.

### checks.yaml

```yaml
mitigated:
  - type: http
    url: http://{{vm}}/health
    expect_status: 200
  - type: promql
    expr: sum(rate(http_requests_total{code=~"5.."}[1m])) / sum(rate(http_requests_total[1m])) < 0.01
fixed:
  - type: script
    run: checks/log_level_ok.sh     # exit 0 = pass
  - type: promql
    expr: predict_linear(node_filesystem_avail_bytes{mountpoint="/data"}[10m], 3600) > 0
```

Check types:

| Type | Runs where | Passes when |
| --- | --- | --- |
| `http` | host | Response status and optional body match. |
| `promql` | host, against Prometheus | The expression returns a non-empty result where every sample is truthy. `mitigated` checks must hold for `mitigate_hold`. |
| `script` | in the VM, over the management channel | Exit code 0. Scripts are copied in at check time from the host. |

Use whichever check type is simplest. Learners are trusted, so checks do not need to resist tampering.

### questions.yaml (optional)

```yaml
- id: cause
  prompt: What filled the disk?
  type: choice
  choices:
    - A core dump
    - An application log written at debug level
    - MySQL binary logs
    - The package cache
  answer: 1
- id: process
  prompt: Which process was writing it?
  type: text
  answer_from_var: proc_name    # answer derived from a randomized var
```

Question types: `choice` (single answer), `multi` (set of answers) and `text` (exact match after trimming and lowercasing, or matched against a randomized variable).

## Demo app ("shop")

A small Go service that looks like a real production app: the online shop of Uncle Wally's Peanut Emporium (see the README). All scenarios break this app or its environment, so contributors only write the fault.

Requirements:

- Endpoints: `GET /health`, `GET /products`, `GET /products/{id}`, `POST /orders`, `GET /orders/{id}`, plus `/metrics` and `net/http/pprof` on a separate admin port.
- Reads go through a Redis cache with a TTL. Writes go to MySQL.
- A background worker processes orders from a queue table in MySQL.
- A cron job does nightly-style maintenance (reporting query, cleanup). Scenarios can change its schedule or behavior.
- Structured logs to a file and to journald. Log level is set by config.
- Config comes from `/etc/shop/shop.env` and a systemd unit. Settings include log level, DB pool size, DB DSNs for source and replica, whether reads go to the replica, Redis address, cache TTL, worker concurrency, client timeouts, and retry policy.
- Metrics: `http_requests_total{route,code}`, `http_request_duration_seconds` histogram, DB pool stats (open, in use, wait count, wait duration), cache hits and misses, worker queue depth and processing rate, and Go runtime metrics.
- Uses `github.com/prometheus/client_golang`.

How faults are injected:

- **Operational faults** come through config and the environment, the way real incidents happen: a bad value in `shop.env`, a changed systemd unit, a cron entry, a firewall rule, a MySQL setting.
- **Code faults** (a memory leak, a connection leak on an error path, retries without backoff) come from alternate builds of the app, selected with Go build tags and deployed by the break script as "the new version." Never add obviously named switches like `LEAK=true` to config. Learners should find code faults with pprof, metrics and logs, the way they would in production.
- Faulty builds must behave identically to the good build except for the fault.

## Images

### Single-node

- Base: Ubuntu 26.04 LTS, amd64 and arm64 so Apple Silicon works. (Changed from Debian; see decisions.md.)
- Installed: nginx (reverse proxy to the app), shop app and worker (systemd units), MySQL 8.4 LTS with `performance_schema` and the slow query log enabled, Redis, cron, node\_exporter, process-exporter, mysqld\_exporter, redis\_exporter, Grafana Alloy, and the usual debugging tools (`strace`, `lsof`, `iostat`/`sysstat`, `tcpdump`, `dig`, `ss`, `htop`, `perf` if available).
- Seed data: a product catalog and order history large enough that a missing index is visibly slow (target: a few million order rows).
- Built once and cached. `opsschool start` should not reinstall packages.

### Multi-node (M5)

- Three app nodes behind HAProxy.
- MySQL source/replica pair with GTID-based replication.
- A 3-node etcd cluster the app uses for leases and config.
- Target memory: 6–8 GB total.

## Telemetry

- Started by the CLI with Docker Compose on the host.
- Prometheus scrapes every 5s. Loki receives logs from Alloy.
- Grafana is provisioned with the base dashboard plus the scenario's `dashboard.json` if present. Anonymous access, refresh every 5s, opens to the scenario dashboard.

The base dashboard is in every scenario and has these rows:

1. **Tier status:** `opsschool_check_passed{tier}` for each tier, plus elapsed time.
2. **Edge:** what customers see at the load balancer: requests by status, error rate, latency.
3. **Service (RED):** request rate, error rate and latency percentiles by route.
4. **Host (USE):** CPU, memory, swap, disk space and inodes, disk I/O and iowait, network, load.
5. **MySQL:** connections, threads running, queries per second, slow queries, InnoDB row lock waits, replica lag.
6. **Redis:** memory, hit rate, connected clients.
7. **Logs:** edge errors, app and system logs from Loki.

The CLI exposes its own metrics endpoint that Prometheus scrapes: `opsschool_check_passed{scenario,tier}` (0 or 1), `opsschool_session_elapsed_seconds` and `opsschool_hints_used`.

## Load generator

- Runs on the host, targets the app through nginx.
- Steady mixed traffic by default (browse, view product, place order), at a rate the single-node image handles comfortably.
- Scenarios can set a load profile in `scenario.yaml`: `steady`, `peak` (periodic bursts) or a custom rate schedule.
- `load_replay` during fix verification runs the scenario's profile at peak for the configured duration.
- `load.new_connections` (0 to 1) sends that share of requests on a fresh connection; the rest reuse idle ones.
- The generator stands in for the edge load balancer: it exports `edge_requests_total` and `edge_request_duration_seconds` (job `edge`) and ships an access log to Loki (`{job="edge"}`). Requests that get no response are recorded as 502 or 504, as a load balancer would return them.

## Results and scoring

- Results are appended to `~/.opsschool/results.jsonl`: username, scenario ID, seed, start and end times, per-tier pass times, whether the free curriculum hint and the paid hint were used, and whether fix verification caused data loss.
- Suggested score per scenario: 100 points per tier passed, minus 10 for the hint (the curriculum link is free), with a time bonus for passing under a scenario's target time. Keep the formula in one place so it can change.
- Collateral damage: scenarios can declare `preserve` checks (for example, the orders table row count must not drop). Failing a preserve check caps the `fixed` tier as failed.

## Scenario catalog

The bootstrap set is 24 scenarios, six categories with four levels each; built scenarios now outnumber it at L3, and planned ones take the next free number at their level. MVP is the 12 L1–L2 scenarios, all single-node.

### Linux

| ID | Level | Fault | Mitigate | Fix |
| --- | --- | --- | --- | --- |
| `linux/1.1` | L1 | App log level set to debug; the log fills `/data`, which also holds the MySQL data directory. | Free space, service healthy. | Log level restored, logrotate configured, space trend flat. |
| `linux/2.1` | L2 | `df` shows free space but writes fail. Variant A: deleted log still held open by a process. Variant B: inode exhaustion from millions of session files. Variant chosen by seed. | Writes succeed. | Holder fixed or restarted with rotation that reopens files; for B, session cleanup job in place. |
| `linux/3.1` | L3 | `/data` was unmounted for maintenance while the shop kept running; its logs went into the empty mountpoint on `/` and are hidden once the volume is mounted again. `df` says full, `du` can't find it. | Space freed from under the mountpoint (bind mount). | Shop and worker require `/data` (`RequiresMountsFor=`); survives reboot. |
| `linux/3.2` | L3 | New fstab entry for a data volume is wrong; the shop unit depends on the mount. Service fails after reboot. | Service up. | fstab and unit ordering correct; survives reboot. |
| `linux/4.1` | L4 | Config management lowered the app's cgroup memory limit, a sidecar leaks memory, and `oom_score_adj` points the OOM killer at the app. | App stable for the hold period. | Limit corrected, sidecar leak fixed or contained, OOM priority corrected; survives load replay. |

### Performance

| ID | Level | Fault | Mitigate | Fix |
| --- | --- | --- | --- | --- |
| `performance/1.1` | L1 | A cron job runs a CPU-bound loop every minute. | Latency back under target. | Cron entry removed or fixed. |
| `performance/2.1` | L2 | Worker concurrency raised past available memory; the box swaps. | Latency back under target. | Concurrency right-sized; no swap-in under load replay. |
| `performance/3.1` | L3 | A cost-control change puts the shop and worker in a systemd slice with a 4–6% CPU quota. The host looks idle; the shop is throttled. | Latency back under target. | Quota removed or raised in the slice unit; survives reboot. |
| `performance/3.2` | L3 | A backup job every 10 minutes saturates disk I/O; p99 spikes while CPU looks normal. | Spikes stop. | Backup throttled (ionice, rate limit) or rescheduled; p99 stable across two backup cycles. |
| `performance/4.1` | L4 | DB pool too small for peak load; requests queue and time out, and client retries double the load. Only appears under peak. | Error rate under target at peak. | Pool sized correctly and retry policy uses backoff with a budget; survives peak replay. |

### Networking

| ID | Level | Fault | Mitigate | Fix |
| --- | --- | --- | --- | --- |
| `networking/1.1` | L1 | `resolv.conf` points to a dead resolver after a simulated DHCP change. Outbound calls by hostname fail. | Resolution works. | Resolver config fixed at its source so it survives reboot. |
| `networking/2.1` | L2 | An iptables rule drops traffic to the app port from the proxy. SSH works. | Traffic flows. | Rule removed from the persisted firewall config; survives reboot. |
| `networking/3.1` | L3 | A stateful firewall rule turns on connection tracking, and a tuning change caps the table at about 1000 entries. At peak the kernel drops packets: 10 s hangs and errors that only the edge load balancer sees. | Errors stop. | Limit sized in the sysctl file; firewall rule kept; survives reboot. |
| `networking/3.2` | L3 | Wrong MAC for the payments host on the service segment: a stale static neighbor with swapped digits, or a decommissioned host announcing the same IP. | Checkout works. | Pin removed or old host retired; ARP learns the right MAC after a reboot. |
| `networking/3.3` | L3 | App's outbound client has keepalive disabled; TIME\_WAIT sockets exhaust ephemeral ports under load. Variant: conntrack table full. | Errors stop. | Keepalive or pooling enabled; survives load replay. |
| `networking/4.1` | L4 | A tunnel lowers the MTU and ICMP is filtered, so path MTU discovery fails. Small responses work, large ones hang. | Large responses complete. | MTU corrected or MSS clamped, and ICMP fragmentation-needed allowed. |

### Databases (MySQL)

| ID | Level | Fault | Mitigate | Fix |
| --- | --- | --- | --- | --- |
| `databases/1.1` | L1 | New app build leaks a connection on an error path until MySQL hits `max_connections` (ERROR 1040). Variant: disk full from binlogs with no expiry. | Connections available, errors stop. | Leak fixed (good build deployed or code corrected); connections stable under load replay. |
| `databases/2.1` | L2 | A migration dropped an index; one endpoint slows sharply. | Latency back under target. | Index restored; slow query rate at baseline. |
| `databases/3.1` | L3 | A tuning change moves MySQL's `tmpdir`; AppArmor denies it, so MySQL won't start though permissions look right. | MySQL running. | Directory allowed in the profile's local additions (or change reverted) with the profile still enforced; survives reboot. |
| `databases/3.2` | L3 | `ALTER TABLE` waits on a metadata lock held by a forgotten open transaction; every later query on the table queues behind it. | Queries on the table complete. | Idle transaction found and the app code path that leaves it open fixed. |
| `databases/4.1` | L4 | A table has no primary key, so with row-based replication the replica scans per row event and lags during a batch job. The app reads its own writes from the replica. | Users see their own writes. | Primary key added and read-your-writes handled (reads after writes go to the source). |

### Production services

| ID | Level | Fault | Mitigate | Fix |
| --- | --- | --- | --- | --- |
| `services/1.1` | L1 | Typo in an env var in `shop.env` after a config change; the service crash-loops. | Service running. | Config corrected. |
| `services/2.1` | L2 | Served cert chain is missing an intermediate, so some clients fail. A second cert expires in 2 days. | All clients connect. | Full chain served and the expiring cert renewed. |
| `services/3.1` | L3 | A config-sync timer reinstalls a bad payments URL from a git repository every five minutes, undoing manual fixes. | Checkout works for longer than the sync interval. | Repository corrected, sync still running. |
| `services/3.2` | L3 | A new app build leaks memory slowly. | Memory stable (rollback). | Leak identified with pprof and the fix deployed. |
| `services/4.1` | L4 | A vendor release of the thumbnail service fills `/` from a corrupt photo. Redis can't save its snapshot and refuses writes, so checkout fails; the shop's data volume has room. | Checkout works (thumbnails stopped, space freed). | Release rolled back or photo quarantined, disk cleared, Redis saving again. |
| `services/4.2` | L4 | Redis restarts, a cache stampede hits MySQL, MySQL saturates, retries without backoff keep it down. | Error rate under target. | Backoff, request coalescing and concurrency limits in place; survives a forced Redis restart during load replay. |

### Distributed systems (multi-node, M5)

| ID | Level | Fault | Mitigate | Fix |
| --- | --- | --- | --- | --- |
| `distributed/1.1` | L1 | HAProxy's health check hits a static path, so a broken app node stays in rotation. | Error rate under target. | Health check uses a real readiness endpoint. |
| `distributed/2.1` | L2 | One node's clock drifts with NTP disabled; token validation fails and leases flap. | Errors stop. | NTP restored; skew alert added. |
| `distributed/3.1` | L3 | One etcd member's disk has high fsync latency; elections flap and config reads time out. | Config reads succeed. | Disk issue fixed or member replaced. |
| `distributed/4.1` | L4 | Brief partial network loss between two nodes triggers retries and queue backlog that keep the system overloaded after the network recovers. | Load shed, errors stop. | Retry budgets, backoff and admission control in place; survives a repeat trigger. |

### Backlog candidates

- Goroutine leak in a new build (find with pprof).
- CPU throttling from GOMAXPROCS set higher than the cgroup CPU limit. Since Go 1.25, GOMAXPROCS respects cgroup limits by default, so the break script sets it explicitly in the unit file.
- GC pressure from a large in-memory cache, with `GOMEMLIMIT` as part of the fix.
- Kubernetes and cloud scenarios (k3s in a VM, LocalStack or similar).

## CI and linting

Every scenario is verified in CI:

1. Boot the image and run `break.sh`. All `mitigated` and `fixed` checks must fail.
2. Run `mitigate.sh`. `mitigated` must pass and `fixed` must still fail.
3. Run `solve.sh`. All tiers must pass after fix verification (restart, reboot if required, load replay).
4. If `questions.yaml` exists, validate it against the randomized variables for three different seeds.

`opsschool validate` lint rules:

- The schema is valid and the ID matches the directory name.
- `break.sh` does not touch the management interface, exporter config, Alloy config or the harness's files.
- Every randomized variable is used in `break.sh`.
- `summary` exists and `curriculum` is a URL.
- `mitigate.sh` and `solve.sh` exist.

CI jobs:

- Every PR: `go build`, `go test`, `go vet`, `opsschool validate` on all scenarios.
- PRs that touch a scenario: the full verification for that scenario. GitHub-hosted Linux runners expose KVM, but confirm this and fall back to a self-hosted runner if needed.

## Conventions

- Go, standard library first. Pinned tool versions.
- Scripts are bash with `set -euo pipefail`, idempotent where possible.
- Scenario text shown to learners describes symptoms only.
- Everything a learner sees at runtime must avoid revealing the scenario's cause: process names, file names and unit names in break scripts should look like normal production components.
- Keep dashboards generated or templated from code where practical; hand-edited Grafana JSON is hard to review.

## Milestones

### M0: Repo and skeleton

- Repo created, this doc committed, `CLAUDE.md`, `CONTRIBUTING.md`, license.
- CLI skeleton with `list`, `validate` and the scenario spec types.
- CI running build, test, vet and validate.

Acceptance: `opsschool validate` passes on a stub scenario and fails with clear messages on broken ones. **Stop for review.**

### M1: Demo app and single-node image

- Shop app with all endpoints, metrics, pprof, logs and config listed above.
- Single-node Lima image with everything installed and seeded.

Acceptance: the image boots, the app serves traffic through nginx, and all exporters respond.

### M2: Telemetry and load

- Compose stack, Prometheus scrape config, Loki, Alloy in the VM, base dashboard.
- Load generator with `steady` and `peak` profiles.

Acceptance: a fresh `start` shows populated dashboards within 30s of the VM being up.

### M3: Harness and first scenario

- `start`, `shell`, `status`, `hint`, `verify`, `quiz`, `stop`, `test`.
- Check engine for all three check types, results store, scoring, tier notifications.
- `linux/1.1` (disk full) implemented end to end, including CI verification.
- `docs/writing-scenarios.md` using `linux/1.1` as the worked example.

Acceptance: a learner can complete `linux/1.1` start to finish, see tier changes on the dashboard, and find the result in `results.jsonl`. `opsschool test scenarios/linux/1.1` passes. **Stop for review.**

### M4: Remaining L1–L2 scenarios

The other 11 single-node L1–L2 scenarios, each passing CI verification.

### M5: Multi-node and L3–L4

Multi-node image, the distributed category, and all L3–L4 scenarios.

### Later phases (not in scope now)

- Hosted mode: Firecracker microVMs, browser terminal, accounts, hosted scoreboard.
- Kubernetes and cloud scenarios.
- A software-engineer track framed around "your service is paging," with the app code in scope.

## Open questions

- Whether to support Vagrant in addition to Lima.
- Where the hosted scoreboard will live, if results should be submittable before hosted mode exists.
