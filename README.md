# Ops School emulator

[![CI](https://github.com/opsschool/emulator/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/opsschool/emulator/actions/workflows/ci.yml)
[![Go version](https://img.shields.io/github/go-mod/go-version/opsschool/emulator)](go.mod)
[![License](https://img.shields.io/github/license/opsschool/emulator)](LICENSE)

Start a production environment in a broken state, debug it with real tools,
and get graded as you go. A hands-on companion to the
[Ops School curriculum](https://www.opsschool.org).

## You're on call

You are the SRE at Uncle Wally's Peanut Emporium, an online shop that sells
peanuts, peanut butter and everything in between to customers who take their
legumes seriously. Customers reach the shop through a load balancer, and the
whole business runs on one Linux server behind it: nginx, the shop's API, a
worker that processes paid orders, a service that makes catalog thumbnails,
MySQL for the catalog and order history, Redis for the cache and customers'
wishlists, and calls out to a payments service run by another team. Wally
wrote most of it himself. Developers, a security team and a network team
keep changing it.

Your job is to keep customers browsing and checking out. When something
breaks, you get paged: an alert fires, and someone tells you what customers
are seeing. You have a root shell on the server and the dashboards. Get
orders flowing again first. Then find what actually broke and fix it, so it
stays fixed through a restart, a reboot and the next rush of customers.
Don't lose anyone's order along the way: Wally counts them every night.

Nobody will tell you what's wrong, and whoever made the last change has gone
home. You have the logs, the metrics and the machine itself. Good luck.

```
opsschool start linux/1.1 --user jdoe
```

You get a shell on the server, a Grafana dashboard with live metrics and
logs (including what the load balancer saw), and a notification as you pass
each tier:

| Tier | Passes when |
| --- | --- |
| `mitigated` | The service is healthy again and stays healthy for the hold period. |
| `fixed` | The cause is gone, and the service survives a restart, a reboot if needed, and a load replay. |

Scenarios range from L1 (one obvious fault) to L4 (several interacting
faults). Juniors aim to mitigate L1–L2 scenarios; seniors fix L3–L4 ones.

If you get stuck, `opsschool hint` first links to the part of the Ops School
curriculum that covers the topic. The first hint is free if you need it.
Asking again gives one more specific hint, which costs 10 points.
Your results record whether you used any hints. Hints are not a bad thing - we
all need them from time to time!

## Getting started

You need Go (the version in `go.mod`; set the `GOTOOLCHAIN=auto` environment
variable to automatically fetch what you need),
Docker with Compose (for the dashboards), and [Lima](https://lima-vm.io) 1.1
or later for the scenario VM.

```
go build -o bin/opsschool ./cmd/opsschool
bin/opsschool image build single-node        # run this once, it takes 10-20 minutes
bin/opsschool list
bin/opsschool start linux/1.1 --user jdoe
```

You only build the image once. If you pull changes later that need a new
image, `opsschool start` tells you and gives you the command to rebuild it.

Once the session has started, open http://127.0.0.1:19999 in your browser.
Everything you need is on that page: a terminal on the server, the
dashboards, your progress and the hints. When you think you've fixed things,
press "Verify my fix".

If you'd rather stay in your own terminal, these commands do the same things:

```
bin/opsschool shell                          # begin debugging as root in the VM
bin/opsschool status                         # status, time, hint used
bin/opsschool hint                           # curriculum link (free), then a hint (-10)
bin/opsschool verify                         # test your fix, can be run repeatedly
bin/opsschool quiz                           # optional, not scored
bin/opsschool stop                           # record the result, tear down
```

The full Grafana dashboards are at http://127.0.0.1:13000 and results go to
`~/.opsschool/results.jsonl`.

Without Lima (for example in CI or a VM without nested virtualization),
pass `--driver container` to `image build`, `start` and `test`. It runs the
scenario machine as a privileged systemd container; see
[docs/decisions.md](docs/decisions.md) for how it differs.

## Status

Milestones M0 to M4 from [docs/design.md](docs/design.md) are built, and
work on L3–L4 has started. There are 17 single-node scenarios:

| Category | L1 | L2 | L3 | L4 |
| --- | --- | --- | --- | --- |
| linux | 1.1 | 2.1 | 3.1 | |
| performance | 1.1 | 2.1 | 3.1 | |
| networking | 1.1 | 2.1 | 3.1, 3.2 | |
| databases | 1.1 | 2.1 | 3.1 | |
| services | 1.1 | 2.1 | 3.1 | 4.1 |

All of them pass `opsschool test` with the Lima driver. Multi-node scenarios
(M5) are not built yet.

## Writing scenarios

See [docs/writing-scenarios.md](docs/writing-scenarios.md) and [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache 2.0. See [LICENSE](LICENSE).
