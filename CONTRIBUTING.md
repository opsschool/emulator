# Contributing

Most contributions are new scenarios. Each scenario is one directory under
`scenarios/<category>/<id>/`, and CI verifies it end to end: the break must
break the checks, the reference mitigation must pass `mitigated`, and the
reference fix must pass `fixed`.

## Adding a scenario

1. Pick an ID: `<category-prefix>-<short-name>`, lowercase with hyphens, for
   example `net-dns`. The directory name must equal the ID.
2. Copy an existing scenario, such as `scenarios/linux/linux-disk-full`, and
   edit it. The format is in [docs/design.md](docs/design.md) under
   "Scenario spec".
3. Run `go run ./cmd/opsschool validate scenarios/<category>/<id>` until it
   reports no errors.
4. Run `make check`.
5. Open a pull request.

## Rules for scenarios

- `summary` describes symptoms, the way a page or a customer report would.
  Never the cause.
- Name things in break scripts the way a real production system would. A cron
  job called `cpu-burner` gives the game away; `report-cache-warm` does not.
- Never touch the management channel from `break.sh`: `eth0`, SSH, exporters,
  Grafana Alloy, or harness files. `opsschool validate` enforces this. If a
  line is safe but trips a rule, add `# lint:allow <rule>` to it and explain
  why in the pull request.
- Every randomized variable must be used by `break.sh`.
- Scripts are bash and start with `set -euo pipefail`.

## Code changes

- Go, standard library first.
- Run `make check` before pushing. CI runs the same checks.
- Record design changes and judgment calls in
  [docs/decisions.md](docs/decisions.md).
