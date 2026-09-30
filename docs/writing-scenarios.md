# Writing scenarios

A scenario breaks the shop, or the machine it runs on, the way a real
incident would. You write the fault, a reference mitigation, a reference fix
and the checks that tell them apart. CI proves all four agree.

This guide builds [`linux-disk-full`](../scenarios/linux/linux-disk-full) as
the worked example. The full format is in [design.md](design.md), "Scenario
spec".

## 1. Pick the incident

Start from something that happens in production and has a clear mitigation
and a clear fix that are different from each other.

`linux-disk-full`: someone turned on debug logging to chase a bug and forgot
to turn it off. The verbose log fills `/data`, which also holds the MySQL
data directory. Orders fail because MySQL can't write, while browsing keeps
working from the Redis cache.

- **Mitigation:** free space (truncate the log). The service recovers, but the
  log keeps growing.
- **Fix:** restore the log level and add log rotation so it can't happen again.

## 2. Know the machine

Every single-node scenario runs against the same machine:

| What | Where |
| --- | --- |
| Shop API | `shop.service`, `127.0.0.1:8080`, behind nginx on port 80 |
| Worker | `shop-worker.service` |
| Payments stand-in | `shop-payments.service`, `127.0.0.1:8081` |
| Maintenance job | `/etc/cron.d/shop-maintenance` |
| Configuration | `/etc/shop/shop.env` (see `demoapp/internal/config`) |
| Binary | `/opt/shop/current` → `/opt/shop/releases/2.3.0` |
| Alternate builds | `/usr/local/lib/shop-builds/<fault>/shop`, see `demoapp/internal/faults` |
| Data volume | `/data`: MySQL (`/data/mysql`), logs (`/data/log/shop`), sessions |
| MySQL | 8.4, database `shop`, root over the socket |

**Operational faults** change configuration: `shop.env`, a unit file, a cron
entry, a firewall rule, a MySQL setting. **Code faults** deploy one of the
alternate builds as "the new version":

```bash
install -D /usr/local/lib/shop-builds/connleak/shop /opt/shop/releases/2.4.0/shop
ln -sfn /opt/shop/releases/2.4.0 /opt/shop/current
systemctl restart shop
```

To add a code fault, add a build tag in `demoapp/internal/faults` and use its
constant where the bug belongs. The faulty build must behave exactly like the
good one apart from the fault.

## 3. scenario.yaml

```yaml
id: linux-disk-full
title: Disk full
category: linux
level: 1
image: single-node
curriculum: https://ops-school.readthedocs.io/en/latest/filesystems_101.html
summary: >
  Orders are failing. Customers report errors at checkout.
  Browsing the catalog still works.
randomize:
  log_name:
    choices: [debug.log, trace.log, app-verbose.log]
fix_verification:
  restart: [shop.service]
  reboot: false
  load_replay: 120s
mitigate_hold: 60s
time_limit: 45m
target_time: 20m
```

- The directory name must equal `id`, under the category's directory.
- `summary` is all the learner sees. Describe symptoms, like a page would.
- `randomize` gives each session different details, so a second attempt is
  not identical. Scripts get each value as `OPSSCHOOL_VAR_<NAME>`.
- `fix_verification` says what `opsschool verify` does before grading `fixed`.
  Restart what a real fix would have to survive. Set `reboot: true` when the
  fix must persist across boots (fstab, firewall rules, resolver config).

## 4. break.sh

Runs as root in the machine after the telemetry and load are up. Keep it
realistic: the learner should be able to find what happened with the tools a
real engineer would use.

```bash
#!/usr/bin/env bash
set -euo pipefail
log_file="/data/log/shop/$OPSSCHOOL_VAR_LOG_NAME"
sed -i -e 's/^SHOP_LOG_LEVEL=.*/SHOP_LOG_LEVEL=debug/' \
  -e "s|^SHOP_LOG_FILE=.*|SHOP_LOG_FILE=$log_file|" /etc/shop/shop.env
systemctl restart shop.service
# ...fast-forward a few hours of debug logging...
```

Rules, enforced by `opsschool validate`:

- Bash with `set -euo pipefail`.
- Use every randomized variable.
- Don't touch the management channel: `eth0`, SSH, the exporters, Alloy or
  `/var/lib/opsschool`. Grading and dashboards depend on them. A line that is
  safe but trips a rule can carry `# lint:allow <rule>`.
- Name things like real production components.

## 5. checks.yaml

```yaml
mitigated:
  - name: health endpoint is up
    type: http
    url: http://{{vm}}/health
    expect_status: 200
  - name: order error rate under 1%
    type: promql
    expr: >
      (sum(rate(http_requests_total{route="POST /orders",code=~"5.."}[1m])) or vector(0))
      / sum(rate(http_requests_total{route="POST /orders"}[1m])) < 0.01
fixed:
  - name: log level is not debug
    type: script
    run: checks/log_level_ok.sh
  - name: shop logs are rotated
    type: script
    run: checks/logrotate_ok.sh
  - name: free space on /data is not trending to zero
    type: promql
    expr: predict_linear(node_filesystem_avail_bytes{mountpoint="/data"}[10m], 3600) > 0
preserve:
  - name: no orders lost
    type: script
    run: checks/orders_preserved.sh
```

- **mitigated** is checked every 5 seconds and passes once every check has
  held for `mitigate_hold`. Test what users feel: health, error rate, latency.
- **fixed** is checked by `opsschool verify`, after the restarts and load
  replay. It passes only when the fixed checks, the mitigated checks and the
  preserve checks all pass. Test that the cause is gone, not just the symptom.
- **preserve** catches collateral damage, such as deleting the orders table
  to free space. Preserve scripts run once with `OPSSCHOOL_PHASE=baseline`
  before the break (store what you need in `$OPSSCHOOL_STATE_DIR`) and with
  `OPSSCHOOL_PHASE=check` during verification.

Check types: `http` (from the host), `promql` (non-empty result, every sample
non-zero) and `script` (runs as root in the machine; exit 0 passes). Templates:
`{{vm}}` (the shop through nginx), `{{vm_admin}}` (the shop's metrics and
pprof port), `{{prometheus}}` and `{{var.<name>}}`.

Metric names to build on: `http_requests_total{route,code}`,
`http_request_duration_seconds`, `shop_db_pool_*`, `shop_cache_requests_total`,
`shop_worker_queue_depth`, plus everything from node_exporter,
process-exporter, mysqld_exporter and redis_exporter.

## 6. mitigate.sh and solve.sh

Reference answers, used only by CI. `mitigate.sh` must pass `mitigated` and
leave `fixed` failing, which proves the tiers measure different things.
`solve.sh` must pass everything.

## 7. hints.md, SOLUTION.md, questions.yaml

- `hints.md`: ordered hints separated by `---` lines, from a nudge to nearly
  the answer. Each costs the learner 10 points.
- `SOLUTION.md`: what happened, how to mitigate, how to fix, and links to the
  curriculum.
- `questions.yaml` (optional): a short quiz for learning. It does not affect
  the score. `answer_from_var` checks an answer against a randomized value.

## 8. Test it

```
opsschool validate scenarios/linux/linux-disk-full
opsschool test scenarios/linux/linux-disk-full
```

`opsschool test` builds a fresh machine and runs the CI sequence:

1. `break.sh`: `mitigated` and `fixed` must both fail.
2. `mitigate.sh`: `mitigated` must pass and hold; `fixed` must still fail.
3. `solve.sh`, then fix verification: everything must pass.
4. Quiz answers must resolve for three seeds.

It needs a built base image: `opsschool image build single-node`. Without
Lima, add `--driver container` to both commands.

Then play it for real: `opsschool start linux-disk-full --user you`.
