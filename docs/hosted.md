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
              │  creates, then reaches over SSH
              ▼
            machine: the scenario server, a KubeVirt VM booted from
            the same disk as the CLI's VM
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
- [KubeVirt](https://kubevirt.io/) (tested with 1.9), on nodes with `/dev/kvm`.
  Cloud VMs usually don't have it: on EKS, use metal instance types (such
  as `c7i.metal-24xl`) for the node group that runs machines. Fargate and
  ECS can't run KubeVirt.
- About 3 CPUs and 5.5 GB of memory per running session: 2 CPUs and 4 GB
  for the VM, the rest for the runner and its telemetry.
- About 6 GB of ephemeral storage per session on the VM's node. The VM
  writes to a copy of its disk there, and some scenarios fill it.
- A registry the cluster can pull from.

No KVM nodes? See "EC2 machines" and "Container machines" below.

## Build and push the images

There are two images. The opsschool image runs the portal and the session
runners. The machine image holds the scenario server's disk, as a KubeVirt
containerDisk. Building it needs Lima, as for the CLI, plus Docker and
`qemu-img`:

```
REG=registry.example.com/opsschool

docker build -f deploy/Dockerfile -t $REG/opsschool:v1 .
docker push $REG/opsschool:v1

go build -o bin/opsschool ./cmd/opsschool
bin/opsschool image build single-node                    # the Lima VM, 10-20 minutes
bin/opsschool image build single-node --driver kubevirt  # exports its disk, a few minutes
docker tag opsschool/single-node-vm:base $REG/single-node-vm:base
docker push $REG/single-node-vm:base
```

The machine image is about 2 GB. The first session on each node waits for
the pull, and the session page says so. To avoid that wait, pre-pull the
image on your nodes, for example with a DaemonSet.

## Deploy

The manifests are in `deploy/kubernetes`. Point them at your images, then
apply them:

```
cd deploy/kubernetes
kustomize edit set image opsschool/opsschool:dev=$REG/opsschool:v1
# Also set OPSSCHOOL_IMAGE in portal.yaml to the same image, and
# --machine-image to $REG/{image}-vm:base.
kubectl apply -k .
```

That creates:

- the `opsschool` namespace, labelled to allow privileged pods
- service accounts for the portal and session pods, with only the pod and
  VirtualMachineInstance permissions they need in that namespace
- the portal Deployment, its Service on port 8080 and a 1 GB volume for
  results
- network policies: session pods take traffic only from the portal, and
  machines only from session pods

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

A learner is root on their scenario machine. With KubeVirt that's root in
a VM with its own kernel, which is as contained as root gets. The network
policies keep a machine from reaching other sessions; your cluster's
network plugin has to enforce NetworkPolicy for that to work.

## Limits and settings

`opsschool serve` takes these flags, set in `portal.yaml`:

| Flag | Default | |
| --- | --- | --- |
| `--max-sessions` | 20 | Most sessions at once. Past it, people are asked to try again in a few minutes. |
| `--max-age` | 3h | Delete any session older than this. |
| `--machines` | kubevirt | `kubevirt` for VMs, `ec2` for EC2 instances, or `pods` for container machines. |
| `--machine-image` | `opsschool/{image}-vm:base` | The machine image; `{image}` becomes the scenario's image. |
| `--machine-cpu` | 1 | CPU request for each machine. The VM always has 2 CPUs and 4 GB. |
| `--machine-memory`, `--machine-memory-limit` | 2Gi, none | Container machines only. |
| `--runtime-class` | none | Container machines only: a runtime class such as `kata`. |
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

## EC2 machines

If your cluster can't run VMs, for example EKS without metal nodes, each
session can get an EC2 instance instead. It's the same machine as the
KubeVirt VM, built as an AMI.

Build the AMI from a machine with AWS credentials. The build starts an
instance from Canonical's Ubuntu 26.04 AMI, provisions it as the Lima
build does, saves it as an AMI and terminates it. The machine you run
the build from must reach the instance on port 22, so give it a subnet
with public addresses and a security group that lets you in, or run the
build from inside the VPC:

```
bin/opsschool image build single-node --driver ec2 \
  --subnet subnet-0123 --security-groups sg-0123   # 20-30 minutes
```

The AMI is tagged `opsschool:image=single-node` and with the image's
fingerprint. Sessions use the newest one in the account and region.

Then run the portal with:

```
- --machines=ec2
- --ec2-subnet=subnet-0456
- --ec2-security-groups=sg-0456
```

- Session pods start and stop the instances, so the `opsschool-session`
  service account needs an IAM role (IRSA or EKS Pod Identity) that allows
  `ec2:RunInstances`, `ec2:CreateTags`, `ec2:DescribeInstances`,
  `ec2:DescribeImages` and `ec2:TerminateInstances`. Limit
  `TerminateInstances` to instances tagged `opsschool:session`.
- The instances' security group must let session pods reach them on
  port 22 (SSH), 80 and the exporters (9091, 9092, 9100, 9104, 9121,
  9256). The session pods must accept the instances on port 13100, where
  their logs go: in `networkpolicy.yaml`, uncomment the `ipBlock` and set
  it to the instances' subnet.
- Each instance is a `c7i.large` (2 vCPUs, 4 GB), the size of the Lima
  VM. Change it with `--ec2-instance-type`, but keep 4 GB of memory: some
  scenarios depend on it.
- The runner terminates the instance when the session ends. If the runner
  dies first, the instance powers itself off, which terminates it, half an
  hour after `--max-age`.

## Container machines

On a cluster without KubeVirt, pass `--machines=pods` and
`--machine-image=$REG/{image}:base`, built with `opsschool image build
single-node --driver container` (about 5 GB). Each machine is then a
privileged systemd container. It's lighter (about 1.5 CPUs and 3 GB per
session) but has limits:

- Root in a privileged container can reach the node, so treat the machines
  as untrusted. Run them in their own kernel if you can: with
  [Kata Containers](https://katacontainers.io/) installed, pass
  `--runtime-class=kata`. Otherwise give them nodes of their own, with
  nothing else on them.
- Scenarios that need a real machine, such as those that change kernel
  settings or fill the disk, say so with `needs_vm` in their
  `scenario.yaml`. The portal leaves them out and logs each one it skips
  when it starts. See "Container driver for development and CI" in
  [decisions.md](decisions.md).
- A reboot restarts the machine's userspace (`systemctl soft-reboot`), not
  its kernel.
- If your nodes run MySQL, or anything else with an AppArmor profile for
  `/usr/sbin/mysqld`, the profile applies inside the machine pods too and
  stops the shop's database. Unload it on those nodes, or keep the machine
  pods on nodes without it.

## When something goes wrong

- `kubectl -n opsschool logs deploy/opsschool` shows sessions starting,
  scoring and ending. If a session stops with an error, the reason is
  logged here; learners only see that something went wrong, because the
  reason can give the scenario away.
- `kubectl -n opsschool get pods,vmi` lists each session's pod,
  `opsschool-session-<id>`, and its VM, `opsschool-machine-<id>`. If the VM
  doesn't start, `kubectl -n opsschool describe vmi opsschool-machine-<id>`
  usually says why; a missing `/dev/kvm` shows up as the launcher pod
  never being scheduled.
- If VMs crash a few seconds into boot with `KVM: entry failed, hardware
  error` in the launcher pod's `compute` log, the node is itself a VM and
  its hypervisor doesn't support KubeVirt's guests. We've seen this on
  kind under WSL2 on AMD. Use metal nodes, or, for a local test cluster
  only, turn on KubeVirt's software emulation, which works but boots
  several times slower:
  `kubectl -n kubevirt patch kv kubevirt --type merge -p
  '{"spec":{"configuration":{"developerConfiguration":{"useEmulation":true}}}}'`
- `kubectl -n opsschool logs opsschool-session-<id> -c runner` shows a
  session's progress while it runs.
