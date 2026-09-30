# Decisions

Changes to [design.md](design.md) and judgment calls made while building.
Newest first.

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

Apache 2.0 for the code. The curriculum uses CC BY 3.0, which suits prose but
not software. Confirm with the project owner.
