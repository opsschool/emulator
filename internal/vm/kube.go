package vm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Kube runs the scenario machine as a privileged systemd pod, for hosted
// mode: the session runner pod drives it with kubectl. The machine is the
// container driver's image, pulled from a registry, so it has the container
// driver's differences from the VM, and one more: a pod's container can't
// restart and keep its files, so a reboot is a systemd soft reboot (a
// userspace restart). See docs/decisions.md, "Hosted mode".
type Kube struct {
	Namespace string
	// Pod is the machine pod's name.
	Pod string
	// Image is the machine image; "{image}" is replaced with the scenario's
	// image name, as in "registry.example.com/opsschool/{image}:base".
	Image string
	// Owner is the pod that owns the machine (the session runner), so
	// deleting it deletes the machine. Optional.
	OwnerName, OwnerUID string
	// LogHost is the address Alloy on the machine pushes logs to: the
	// runner pod, where Loki runs.
	LogHost string
	// Session labels the pod.
	Session string
	// RuntimeClass runs the machine under another runtime, such as Kata
	// Containers, which gives it its own kernel. Optional.
	RuntimeClass string
	// Requests and limits. Empty values are left out.
	CPU, Memory, MemoryLimit string
}

// KubeFromEnv configures the driver from the environment the portal gives
// a session runner pod.
func KubeFromEnv() (*Kube, error) {
	k := &Kube{
		Namespace:    os.Getenv("OPSSCHOOL_NAMESPACE"),
		Pod:          os.Getenv("OPSSCHOOL_MACHINE_POD"),
		Image:        os.Getenv("OPSSCHOOL_MACHINE_IMAGE"),
		OwnerName:    os.Getenv("OPSSCHOOL_POD_NAME"),
		OwnerUID:     os.Getenv("OPSSCHOOL_POD_UID"),
		LogHost:      os.Getenv("OPSSCHOOL_POD_IP"),
		Session:      os.Getenv("OPSSCHOOL_SESSION"),
		RuntimeClass: os.Getenv("OPSSCHOOL_RUNTIME_CLASS"),
		CPU:          os.Getenv("OPSSCHOOL_MACHINE_CPU"),
		Memory:       os.Getenv("OPSSCHOOL_MACHINE_MEMORY"),
		MemoryLimit:  os.Getenv("OPSSCHOOL_MACHINE_MEMORY_LIMIT"),
	}
	if k.Namespace == "" {
		b, _ := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		k.Namespace = strings.TrimSpace(string(b))
	}
	if k.Image == "" {
		k.Image = "opsschool/{image}:base"
	}
	if k.Namespace == "" || k.Pod == "" || k.LogHost == "" {
		return nil, fmt.Errorf("the kubernetes driver runs inside a session pod; OPSSCHOOL_NAMESPACE, OPSSCHOOL_MACHINE_POD and OPSSCHOOL_POD_IP must be set")
	}
	return k, nil
}

func (k *Kube) Name() string             { return "kubernetes" }
func (k *Kube) TelemetryNetwork() string { return "" }

// Base can't see the registry's images. A missing image fails the pod's
// image pull instead, which Create reports.
func (k *Kube) Base(ctx context.Context, image string) (bool, string, error) {
	return true, "", nil
}

func (k *Kube) kubectl(ctx context.Context, stdin io.Reader, args ...string) (Result, error) {
	return run(ctx, stdin, "kubectl", append([]string{"--namespace", k.Namespace}, args...)...)
}

func (k *Kube) mustKubectl(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	res, err := k.kubectl(ctx, stdin, args...)
	if err != nil {
		return "", fmt.Errorf("kubectl %s: %w", strings.Join(args, " "), err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("kubectl %s: exit %d: %s", strings.Join(args, " "), res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	return res.Stdout, nil
}

// MachinePod returns the machine's pod manifest.
func (k *Kube) MachinePod(image string) map[string]any {
	labels := map[string]string{
		"app.kubernetes.io/name":      "opsschool",
		"app.kubernetes.io/component": "machine",
	}
	if k.Session != "" {
		labels["opsschool.org/session"] = k.Session
	}
	meta := map[string]any{"name": k.Pod, "labels": labels}
	if k.OwnerName != "" && k.OwnerUID != "" {
		meta["ownerReferences"] = []any{map[string]any{
			"apiVersion": "v1", "kind": "Pod", "name": k.OwnerName, "uid": k.OwnerUID,
		}}
	}
	resources := map[string]any{}
	requests := map[string]string{}
	if k.CPU != "" {
		requests["cpu"] = k.CPU
	}
	if k.Memory != "" {
		requests["memory"] = k.Memory
	}
	if len(requests) > 0 {
		resources["requests"] = requests
	}
	if k.MemoryLimit != "" {
		resources["limits"] = map[string]string{"memory": k.MemoryLimit}
	}
	memory := map[string]any{"medium": "Memory"}
	spec := map[string]any{
		"hostname": "scenario-vm",
		// A machine that powers off stays off, as a VM would. Restarting
		// the container would bring it back from the image, fault and all.
		"restartPolicy": "Never",
		// The learner is root here: no Kubernetes credentials or service
		// addresses for them to find.
		"automountServiceAccountToken": false,
		"enableServiceLinks":           false,
		// systemd treats SIGTERM as "re-execute", so deletion waits out
		// the grace period; there is nothing to save.
		"terminationGracePeriodSeconds": 5,
		"hostAliases": []any{
			map[string]any{"ip": k.LogHost, "hostnames": []string{"host.lima.internal"}},
			// As the container driver does: the cluster owns resolv.conf,
			// so the site resolver for shop.internal is replaced by hosts.
			map[string]any{"ip": "127.0.0.1", "hostnames": []string{
				"payments.shop.internal", "api.shop.internal", "partners.shop.internal"}},
		},
		"containers": []any{map[string]any{
			"name":            "machine",
			"image":           strings.ReplaceAll(k.Image, "{image}", image),
			"imagePullPolicy": "IfNotPresent",
			"securityContext": map[string]any{"privileged": true},
			"resources":       resources,
			"volumeMounts": []any{
				map[string]any{"name": "run", "mountPath": "/run"},
				map[string]any{"name": "run-lock", "mountPath": "/run/lock"},
			},
		}},
		"volumes": []any{
			map[string]any{"name": "run", "emptyDir": memory},
			map[string]any{"name": "run-lock", "emptyDir": memory},
		},
	}
	if k.RuntimeClass != "" {
		spec["runtimeClassName"] = k.RuntimeClass
	}
	return map[string]any{"apiVersion": "v1", "kind": "Pod", "metadata": meta, "spec": spec}
}

// softReboot makes `reboot` a userspace restart; see Kube. The kubelet
// bind-mounts /etc/hosts, /etc/hostname and /etc/resolv.conf, and a soft
// reboot unmounts them like any other file system, leaving the image's
// empty files behind. Without default dependencies, their mount units
// don't conflict with umount.target, so they stay.
const softReboot = `ln -sf /usr/lib/systemd/system/soft-reboot.target /etc/systemd/system/reboot.target
for f in /etc/hosts /etc/hostname /etc/resolv.conf /dev/termination-log; do
  findmnt -n "$f" >/dev/null || continue
  d=/etc/systemd/system/$(systemd-escape -p --suffix=mount "$f").d
  mkdir -p "$d"
  printf '[Unit]\nDefaultDependencies=no\n' >"$d/kubelet.conf"
done
systemctl daemon-reload`

func (k *Kube) Create(ctx context.Context, image string) error {
	b, err := json.Marshal(k.MachinePod(image))
	if err != nil {
		return err
	}
	if _, err := k.mustKubectl(ctx, bytes.NewReader(b), "create", "-f", "-"); err != nil {
		return err
	}
	if err := k.waitRunning(ctx, 10*time.Minute); err != nil {
		return err
	}
	if err := waitBooted(ctx, k, 3*time.Minute); err != nil {
		return err
	}
	res, err := k.Run(ctx, softReboot, nil)
	if err == nil && res.ExitCode != 0 {
		err = fmt.Errorf("%s", strings.TrimSpace(res.Stderr))
	}
	if err != nil {
		return fmt.Errorf("setting up reboots: %w", err)
	}
	return nil
}

// waitRunning waits for the machine's container to start. The first
// session on a node pulls the image, which takes a few minutes.
func (k *Kube) waitRunning(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		out, err := k.mustKubectl(ctx, nil, "get", "pod", k.Pod, "-o",
			`jsonpath={.status.phase} {.status.containerStatuses[0].state.waiting.reason} {.status.containerStatuses[0].state.waiting.message}`)
		if err != nil {
			return err
		}
		phase, why, _ := strings.Cut(strings.TrimSpace(out), " ")
		switch {
		case phase == "Running":
			return nil
		case phase == "Failed" || phase == "Succeeded":
			return fmt.Errorf("the machine pod stopped (%s)", phase)
		case strings.HasPrefix(why, "ErrImagePull") || strings.HasPrefix(why, "ImagePullBackOff") || strings.HasPrefix(why, "InvalidImageName"):
			return fmt.Errorf("can't pull the machine image: %s", why)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the machine pod did not start within %s (%s)", timeout, strings.TrimSpace(out))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (k *Kube) Exists(ctx context.Context) (bool, error) {
	res, err := k.kubectl(ctx, nil, "get", "pod", k.Pod, "--ignore-not-found", "-o", "name")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(res.Stdout) != "", nil
}

func (k *Kube) execArgs(tty bool, argv ...string) []string {
	flag := "-i"
	if tty {
		flag = "-it"
	}
	return append([]string{"exec", flag, k.Pod, "-c", "machine", "--"}, argv...)
}

func (k *Kube) Run(ctx context.Context, script string, env []string) (Result, error) {
	return k.kubectl(ctx, strings.NewReader(envScript(script, env)), k.execArgs(false, "bash", "-s")...)
}

// CopyIn streams a tar of local through kubectl exec, which only needs tar
// in the machine.
func (k *Kube) CopyIn(ctx context.Context, local, remote string) error {
	var buf bytes.Buffer
	if err := tarTree(&buf, local, remote); err != nil {
		return err
	}
	res, err := k.kubectl(ctx, &buf, k.execArgs(false, "tar", "-x", "--no-overwrite-dir", "-C", "/", "-f", "-")...)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("copy into machine: %s", strings.TrimSpace(res.Stderr))
	}
	return nil
}

func (k *Kube) ShellCommand() []string {
	// kubectl exec passes no TERM, unlike docker exec -t.
	return append([]string{"kubectl", "--namespace", k.Namespace},
		k.execArgs(true, "env", "-C", "/root", "TERM=xterm-256color", "bash", "-l")...)
}

func (k *Kube) Reboot(ctx context.Context) error {
	// The exec's connection may drop as the machine's services stop.
	k.Run(ctx, "systemctl soft-reboot", nil)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
	}
	if err := waitBooted(ctx, k, 3*time.Minute); err != nil {
		return fmt.Errorf("after reboot: %w", err)
	}
	return nil
}

func (k *Kube) Delete(ctx context.Context) error {
	_, err := k.mustKubectl(ctx, nil, "delete", "pod", k.Pod, "--ignore-not-found", "--wait=false")
	return err
}

// PodIP returns the machine pod's address.
func (k *Kube) PodIP(ctx context.Context) (string, error) {
	out, err := k.mustKubectl(ctx, nil, "get", "pod", k.Pod, "-o", "jsonpath={.status.podIP}")
	if err != nil {
		return "", err
	}
	if ip := strings.TrimSpace(out); ip != "" {
		return ip, nil
	}
	return "", fmt.Errorf("the machine pod has no address")
}
