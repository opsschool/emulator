# Ops School emulator

Start a production environment in a broken state, debug it with real tools,
and get graded as you go. A hands-on companion to the
[Ops School curriculum](https://www.opsschool.org).

```
opsschool start linux/1.1 --user jdoe
```

You get a shell on a VM, a Grafana dashboard with live metrics and logs, and a
notification as you pass each tier:

| Tier | Passes when |
| --- | --- |
| `mitigated` | The service is healthy again and stays healthy for the hold period. |
| `fixed` | The cause is gone, and the service survives a restart, a reboot if needed, and a load replay. |

Scenarios range from L1 (one obvious fault) to L4 (several interacting
faults). Juniors aim to mitigate L1–L2 scenarios; seniors fix L3–L4 ones.

## Getting started

You need Go (the version in `go.mod`; `GOTOOLCHAIN=auto` fetches it),
Docker with Compose (for the dashboards), and [Lima](https://lima-vm.io) 1.1
or later for the scenario VM.

```
go build -o bin/opsschool ./cmd/opsschool
bin/opsschool image build single-node        # once, 10-20 minutes
bin/opsschool list
bin/opsschool start linux/1.1 --user jdoe
bin/opsschool shell                          # debug as root in the VM
bin/opsschool status                         # tiers, time, hints
bin/opsschool hint                           # -10 points each
bin/opsschool verify                         # claim a fix
bin/opsschool quiz                           # optional, not scored
bin/opsschool stop                           # record the result, tear down
```

Dashboards are at http://127.0.0.1:13000 and results go to
`~/.opsschool/results.jsonl`.

Without Lima (for example in CI or a VM without nested virtualization),
pass `--driver container` to `image build`, `start` and `test`. It runs the
scenario machine as a privileged systemd container; see
[docs/decisions.md](docs/decisions.md) for how it differs.

## Status

Milestones M0 to M4 from [docs/design.md](docs/design.md) are built: the
ten single-node L1–L2 scenarios, two in each of linux, performance,
networking, databases and services. All pass `opsschool test` with the Lima
driver.

## Writing scenarios

See [docs/writing-scenarios.md](docs/writing-scenarios.md) and [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache 2.0. See [LICENSE](LICENSE).
