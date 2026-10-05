# Running Ops School on Kubernetes

Most people who use Ops School at work won't install a CLI. They'll open a
web page, type their name, pick a scenario and get a terminal on a broken
server in their browser, with a scoreboard the whole group shares. This is
how to run that page, the portal, on a Kubernetes cluster.

The CLI and the portal are both supported ways to use Ops School. They run
the same scenarios, the same checks and the same session page.

## How it works

```
Browser ──▶ portal (Deployment, 1 replica)            results.jsonl on a PVC
              │  creates and proxies to
              ▼
            session pod, one per learner
              ├── runner: plays `opsschool start`, then serves the
              │   session page's API, the terminal and the checks
              ├── Prometheus, Loki, Grafana (sidecars)
              │  creates, then forwards the machine's ports to
              ▼
            machine pod: the scenario server, a privileged systemd
            container built from the same image as `--driver container`
```

- The portal lists scenarios, starts sessions, keeps the scoreboard and
  proxies each learner's browser to their session at `/s/<id>/`.
- The runner does what `opsschool start` does on a laptop: it boots the
  machine, waits for the healthy baseline, applies the break and runs the
  load and the checks. When the session ends it sends the result to the
  portal and deletes the machine.
- A session ends when the learner presses End session, or ten minutes after
  its time limit. The portal deletes anything left over a minute after it
  ends, and any session older than `--max-age` (three hours by default).

## What you need

- Kubernetes 1.29 or later. The telemetry runs as native sidecars, so the
  session pod finishes when the runner does.
- Nodes with cgroup v2, which can run privileged pods. The machine pods are
  privileged because a scenario server needs systemd, iptables and its own
  mounts.
- About 1.5 CPUs and 3 GB of memory per running session: 1 CPU and 2 GB for
  the machine, the rest for the runner and its telemetry.
- A registry the cluster can pull from.

## Build and push the images

There are two images. The opsschool image runs the portal and the session
runners. The machine image is the scenario server.

```
REG=registry.example.com/opsschool

docker build -f deploy/Dockerfile -t $REG/opsschool:v1 .
docker push $REG/opsschool:v1

go build -o bin/opsschool ./cmd/opsschool
bin/opsschool image build single-node --driver container   # 10-20 minutes
docker tag opsschool/single-node:base $REG/single-node:base
docker push $REG/single-node:base
```

The machine image is about 5 GB. The first session on each node waits for
the pull, and the session page says so. To avoid that wait, pre-pull the
image on your nodes, for example with a DaemonSet.

## Deploy

The manifests are in `deploy/kubernetes`. Point them at your images, then
apply them:

```
cd deploy/kubernetes
kustomize edit set image opsschool/opsschool:dev=$REG/opsschool:v1
# Also set OPSSCHOOL_IMAGE in portal.yaml to the same image, and
# --machine-image to $REG/{image}:base.
kubectl apply -k .
```

That creates:

- the `opsschool` namespace, labelled to allow privileged pods
- service accounts for the portal and session pods, with only the pod
  permissions they need in that namespace
- the portal Deployment, its Service on port 8080 and a 1 GB volume for
  results
- network policies: session pods take traffic only from the portal, and
  machine pods only from session pods

Then expose the `opsschool` Service the way you expose other internal
tools, with an Ingress or a Gateway. The terminal uses a WebSocket, so
whatever sits in front must allow WebSocket upgrades on `/s/`.

## Who's playing

By default, people type a name the first time they visit, and it goes on
the scoreboard. Nothing checks it. Learners are trusted: Ops School is for
learning, not for exams.

If you put the portal behind a sign-in proxy (oauth2-proxy, an identity-aware
proxy, your Ingress's auth), pass the header it sets:

```
- --user-header=X-Forwarded-Email
```

The portal then uses that header and never asks for a name. Make sure only
the proxy can reach the portal, or anyone can send the header.

## Isolation

A learner is root on their scenario machine, and the machine is a
privileged pod. Root in a privileged container can reach the node, so treat
the machines as untrusted:

- Run them in their own kernel if you can. With
  [Kata Containers](https://katacontainers.io/) installed, pass
  `--runtime-class=kata`.
- Otherwise, give them nodes of their own (a taint and toleration, or a
  separate node pool) with nothing else on them.
- The network policies keep a machine from reaching other sessions. Your
  cluster's network plugin has to enforce NetworkPolicy for that to work.

## Limits and settings

`opsschool serve` takes these flags, set in `portal.yaml`:

| Flag | Default | |
| --- | --- | --- |
| `--max-sessions` | 20 | Most sessions at once. Past it, people are asked to try again in a few minutes. |
| `--max-age` | 3h | Delete any session older than this. |
| `--machine-cpu`, `--machine-memory` | 1, 2Gi | Requests for each machine pod. |
| `--machine-memory-limit` | none | A limit for each machine pod. Some performance scenarios fill memory on purpose; with a limit the pod is killed instead of slowing down. |
| `--runtime-class` | none | Runtime class for machine pods, such as `kata`. |
| `--user-header` | none | See "Who's playing". |
| `--public-url` | from the request | The portal's address as browsers see it, such as `https://opsschool.example.com`. Grafana needs it. Set it if the proxy in front changes the Host header or doesn't set `X-Forwarded-Proto`. |

Each person can run one session at a time.

## Results

Results go to `results.jsonl` on the portal's volume, one line per finished
session, in the same format as the CLI's `~/.opsschool/results.jsonl`. Back
it up like any other small file. The portal runs as a single replica
because of this file; it holds no other state, and sessions keep running
while it restarts.

## Differences from the CLI

- The quiz is only in the CLI for now.
- A reboot during "Verify my fix" (and `reboot` in the terminal) restarts
  the machine's userspace (`systemctl soft-reboot`), not its kernel. A pod
  that restarts loses its files, and the checks need them to survive.
- The machine is the container image, so the things `--driver container`
  can't do apply here too; see "Container driver for development and CI" in
  [decisions.md](decisions.md).

## When something goes wrong

- `kubectl -n opsschool logs deploy/opsschool` shows sessions starting,
  scoring and ending. If a session stops with an error, the reason is
  logged here; learners only see that something went wrong, because the
  reason can give the scenario away.
- `kubectl -n opsschool get pods` lists each session's pods:
  `opsschool-session-<id>` and `opsschool-machine-<id>`.
- `kubectl -n opsschool logs opsschool-session-<id> -c runner` shows a
  session's progress while it runs.
