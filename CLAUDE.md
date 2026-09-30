# Ops School simulator

Learners start a broken production environment, debug it with real tools, and
get graded. The full spec is [docs/design.md](docs/design.md). Changes to the
spec and judgment calls are in [docs/decisions.md](docs/decisions.md): read it
before relying on the design doc, and add to it when you make a call the spec
doesn't cover.

## Commands

- `make check`: gofmt, vet, tests, shellcheck and `opsschool validate` on all scenarios. Run before every push.
- `go run ./cmd/opsschool validate scenarios`: lint scenarios only.

## Conventions

- Go, standard library first. Pin tool and dependency versions.
- Scripts are bash with `set -euo pipefail`, idempotent where possible.
- Scenario text shown to learners describes symptoms only, never the cause.
- Everything a learner sees at runtime must avoid revealing the cause: process
  names, file names and unit names in break scripts should look like normal
  production components.
- Generate or template Grafana dashboards from code where practical;
  hand-edited Grafana JSON is hard to review.
- Learners are trusted. Grading needs to be accurate, not tamper-proof.
- A scenario's directory name is its ID: `scenarios/<category>/<id>/`.
