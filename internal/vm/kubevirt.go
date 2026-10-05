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

// KubeVirt runs the scenario machine as a KubeVirt virtual machine, for
// hosted mode on clusters with KVM nodes. Unlike Kube, it boots the same
// disk as the Lima driver (packaged by BuildVMDisk), so it has its own
// kernel, disk, memory and network, every scenario works, and a reboot is
// a real one. The session runner pod reaches it over SSH with a key it
// makes for the session. See docs/hosted.md.
type KubeVirt struct {
	Namespace string
	// VMI is the VirtualMachineInstance's name.
	VMI string
	// Image is the container disk image; "{image}" is replaced with the
	// scenario's image name, as in "registry.example.com/opsschool/{image}-vm:base".
	Image string
	// Owner is the pod that owns the machine (the session runner), so
	// deleting it deletes the machine. Optional.
	OwnerName, OwnerUID string
	// LogHost is the address Alloy on the machine pushes logs to: the
	// runner pod, where Loki runs.
	LogHost string
	// Session labels the machine.
	Session string
	// CPU is the CPU request for the VM's pod. Optional.
	CPU string

	sshVM
}

// VMCPUs and VMMemory size a KubeVirt machine as images/<image>/lima.yaml
// sizes a Lima one: some scenarios depend on the memory size.
const (
	VMCPUs   = 2
	VMMemory = "4Gi"
	// vmMAC is the machine's network card address. The network config
	// names the card with this address eth0, as Lima does.
	vmMAC = "52:54:00:4f:53:01"
)

// KubeVirtFromEnv configures the driver from the environment the portal
// gives a session runner pod.
func KubeVirtFromEnv() (*KubeVirt, error) {
	k, err := KubeFromEnv()
	if err != nil {
		return nil, err
	}
	v := &KubeVirt{
		Namespace: k.Namespace, VMI: k.Pod, Image: os.Getenv("OPSSCHOOL_MACHINE_IMAGE"),
		OwnerName: k.OwnerName, OwnerUID: k.OwnerUID, LogHost: k.LogHost, Session: k.Session, CPU: k.CPU,
	}
	if v.Image == "" {
		v.Image = VMDiskImage("{image}")
	}
	v.address = v.Address
	return v, nil
}

func (v *KubeVirt) Name() string             { return "kubevirt" }
func (v *KubeVirt) TelemetryNetwork() string { return "" }

// Base can't see the registry's images. A missing image fails the VM's
// start instead, which Create reports.
func (v *KubeVirt) Base(ctx context.Context, image string) (bool, string, error) {
	return true, "", nil
}

func (v *KubeVirt) kubectl(ctx context.Context, stdin io.Reader, args ...string) (Result, error) {
	return run(ctx, stdin, "kubectl", append([]string{"--namespace", v.Namespace}, args...)...)
}

func (v *KubeVirt) mustKubectl(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	res, err := v.kubectl(ctx, stdin, args...)
	if err != nil {
		return "", fmt.Errorf("kubectl %s: %w", strings.Join(args, " "), err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("kubectl %s: exit %d: %s", strings.Join(args, " "), res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	return res.Stdout, nil
}

const networkConfig = `version: 2
ethernets:
  eth0:
    match:
      macaddress: "` + vmMAC + `"
    set-name: eth0
    dhcp4: true
`

// Manifest returns the VirtualMachineInstance.
func (v *KubeVirt) Manifest(image, pubkey string) map[string]any {
	labels := map[string]string{
		"app.kubernetes.io/name":      "opsschool",
		"app.kubernetes.io/component": "machine",
	}
	if v.Session != "" {
		labels["opsschool.org/session"] = v.Session
	}
	meta := map[string]any{"name": v.VMI, "labels": labels}
	if v.OwnerName != "" && v.OwnerUID != "" {
		meta["ownerReferences"] = []any{map[string]any{
			"apiVersion": "v1", "kind": "Pod", "name": v.OwnerName, "uid": v.OwnerUID,
		}}
	}
	domain := map[string]any{
		"cpu":    map[string]any{"cores": VMCPUs},
		"memory": map[string]any{"guest": VMMemory},
		// The Lima base boots with UEFI.
		"firmware": map[string]any{"bootloader": map[string]any{"efi": map[string]any{"secureBoot": false}}},
		"devices": map[string]any{
			"disks": []any{
				map[string]any{"name": "root", "disk": map[string]any{"bus": "virtio"}},
				map[string]any{"name": "cloudinit", "disk": map[string]any{"bus": "virtio"}},
			},
			"interfaces": []any{map[string]any{"name": "default", "masquerade": map[string]any{}, "macAddress": vmMAC}},
			"rng":        map[string]any{},
		},
	}
	if v.CPU != "" {
		domain["resources"] = map[string]any{"requests": map[string]string{"cpu": v.CPU}}
	}
	spec := map[string]any{
		"domain":                        domain,
		"hostname":                      "scenario-vm",
		"terminationGracePeriodSeconds": 0,
		"networks":                      []any{map[string]any{"name": "default", "pod": map[string]any{}}},
		"volumes": []any{
			map[string]any{"name": "root", "containerDisk": map[string]any{
				"image": strings.ReplaceAll(v.Image, "{image}", image), "imagePullPolicy": "IfNotPresent",
			}},
			map[string]any{"name": "cloudinit", "cloudInitNoCloud": map[string]any{
				"userData": cloudConfig(pubkey, v.LogHost, ""), "networkData": networkConfig,
			}},
		},
	}
	return map[string]any{"apiVersion": "kubevirt.io/v1", "kind": "VirtualMachineInstance", "metadata": meta, "spec": spec}
}

func (v *KubeVirt) Create(ctx context.Context, image string) error {
	if v.address == nil {
		v.address = v.Address
	}
	pub, err := v.newKey(ctx)
	if err != nil {
		return err
	}
	b, err := json.Marshal(v.Manifest(image, pub))
	if err != nil {
		return err
	}
	if _, err := v.mustKubectl(ctx, bytes.NewReader(b), "create", "-f", "-"); err != nil {
		return err
	}
	if err := v.waitRunning(ctx, 15*time.Minute); err != nil {
		return err
	}
	return waitBooted(ctx, v, 5*time.Minute)
}

// waitRunning waits for the VM to start and get an address. The first
// session on a node pulls the disk image, which takes a few minutes.
func (v *KubeVirt) waitRunning(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		out, err := v.mustKubectl(ctx, nil, "get", "vmi", v.VMI, "-o",
			`jsonpath={.status.phase} {.status.interfaces[0].ipAddress}`)
		if err != nil {
			return err
		}
		phase, ip, _ := strings.Cut(strings.TrimSpace(out), " ")
		switch {
		case phase == "Running" && ip != "":
			return nil
		case phase == "Failed" || phase == "Succeeded":
			return fmt.Errorf("the machine stopped (%s): %s", phase, v.launcherProblem(ctx))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the machine did not start within %s (%s): %s", timeout, strings.TrimSpace(out), v.launcherProblem(ctx))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// launcherProblem describes why the VM's pod isn't running, such as a
// failed image pull, for the runner's log.
func (v *KubeVirt) launcherProblem(ctx context.Context) string {
	res, err := v.kubectl(ctx, nil, "get", "pod", "-l", "vm.kubevirt.io/name="+v.VMI, "-o",
		`jsonpath={range .items[*].status.containerStatuses[*]}{.name}: {.state.waiting.reason} {.state.waiting.message}{"\n"}{end}`)
	if err != nil || strings.TrimSpace(res.Stdout) == "" {
		return "no details"
	}
	return strings.TrimSpace(res.Stdout)
}

// Address returns the machine's address, where the runner reaches its
// SSH server and its services.
func (v *KubeVirt) Address(ctx context.Context) (string, error) {
	out, err := v.mustKubectl(ctx, nil, "get", "vmi", v.VMI, "-o", "jsonpath={.status.interfaces[0].ipAddress}")
	if err != nil {
		return "", err
	}
	if ip := strings.TrimSpace(out); ip != "" {
		return ip, nil
	}
	return "", fmt.Errorf("the machine has no address")
}

func (v *KubeVirt) Exists(ctx context.Context) (bool, error) {
	res, err := v.kubectl(ctx, nil, "get", "vmi", v.VMI, "--ignore-not-found", "-o", "name")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(res.Stdout) != "", nil
}

func (v *KubeVirt) Delete(ctx context.Context) error {
	v.removeKey()
	_, err := v.mustKubectl(ctx, nil, "delete", "vmi", v.VMI, "--ignore-not-found", "--wait=false")
	return err
}
