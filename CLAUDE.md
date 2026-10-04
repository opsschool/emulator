# Ops School emulator

Learners start a broken production environment, debug it with real tools, and
get graded. The full spec is [docs/design.md](docs/design.md). Changes to the
spec and judgment calls are in [docs/decisions.md](docs/decisions.md): read it
before relying on the design doc, and add to it when you make a call the spec
doesn't cover.

## Commands

- `make check`: gofmt, vet, tests, shellcheck and `opsschool validate` on all scenarios. Run before every push.
- `go run ./cmd/opsschool validate scenarios`: lint scenarios only.
- `opsschool image build single-node --driver container`, then
  `opsschool test scenarios/<category>/<level>.<n> --driver container`: verify a
  scenario end to end without Lima. Needs Docker and about 10 GB of disk.
- `SHOP_TEST_DSN=... go test ./demoapp/...`: store integration tests
  against a disposable MySQL.

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
- A scenario's `curriculum` link is the learner's free first hint. If the
  curriculum doesn't cover the topic, add a section to
  [opsschool/curriculum](https://github.com/opsschool/curriculum), one PR per
  section, following its style guide (`meta/style_guide.rst`, "Writing"):
  plain English, precise and qualified statements, no headline style.
- Scenarios live in `scenarios/<category>/<level>.<n>/`, for example
  `scenarios/linux/1.1/`, and that path is their ID (`linux/1.1`). The level
  comes from the directory; `n` is the next free number at that level.
  Scenarios have no titles.
