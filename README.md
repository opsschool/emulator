# Ops School simulator

Start a production environment in a broken state, debug it with real tools,
and get graded as you go. A hands-on companion to the
[Ops School curriculum](https://ops-school.readthedocs.io).

```
opsschool start linux-disk-full --user jdoe
```

You get a shell on a VM, a Grafana dashboard with live metrics and logs, and a
notification as you pass each tier:

| Tier | Passes when |
| --- | --- |
| `mitigated` | The service is healthy again and stays healthy for the hold period. |
| `fixed` | The cause is gone, and the service survives a restart, a reboot if needed, and a load replay. |

Scenarios range from L1 (one obvious fault) to L4 (several interacting
faults). Juniors aim to mitigate L1–L2 scenarios; seniors fix L3–L4 ones.

## Status

Under construction. See the milestones in [docs/design.md](docs/design.md).

| Command | Status |
| --- | --- |
| `opsschool list` | works |
| `opsschool validate <path>` | works |
| everything else | in progress |

## Building

Requires Go (the version in `go.mod`; `GOTOOLCHAIN=auto` fetches it).

```
go build -o bin/opsschool ./cmd/opsschool
bin/opsschool list
```

## Writing scenarios

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache 2.0. See [LICENSE](LICENSE).
