package vm

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"
)

// Container runs the scenario machine as a privileged systemd container.
// It shares the host kernel, so scenarios that need their own kernel
// (sysctls, reboots that clear kernel state, cgroup memory pressure on the
// whole machine) are less faithful than under Lima.
type Container struct{}

// Network is the Docker network shared with the telemetry stack.
const Network = "opsschool"

// BaseImage returns the Docker image holding a built base machine.
func BaseImage(image string) string { return "opsschool/" + image + ":base" }

func (c *Container) Name() string             { return "container" }
func (c *Container) TelemetryNetwork() string { return Network }

// FingerprintLabel is the base image label holding its Fingerprint.
const FingerprintLabel = "org.opsschool.fingerprint"

func (c *Container) Base(ctx context.Context, image string) (bool, string, error) {
	res, err := run(ctx, nil, "docker", "image", "inspect", "--format",
		`{{index .Config.Labels "`+FingerprintLabel+`"}}`, BaseImage(image))
	if err != nil || res.ExitCode != 0 {
		return false, "", err
	}
	fp := strings.TrimSpace(res.Stdout)
	if fp == "<no value>" {
		fp = ""
	}
	return true, fp, nil
}

// RunArgs are the docker run flags a systemd machine needs.
func RunArgs(name string) []string {
	// On cgroup v2, systemd gets a private cgroup namespace, as Kubernetes
	// gives it. The host's namespace fails under Docker Desktop, whose
	// daemon sees a different cgroup root from the containers it starts.
	cgroups := []string{"--cgroupns=private"}
	if !cgroupV2() {
		cgroups = []string{"--cgroupns=host", "-v", "/sys/fs/cgroup:/sys/fs/cgroup:rw"}
	}
	return append(append([]string{
		"run", "-d", "--name", name, "--hostname", "scenario-vm", "--privileged",
	}, cgroups...),
		"--tmpfs", "/run", "--tmpfs", "/run/lock",
		// Docker owns /etc/resolv.conf, so the site resolver that the VM
		// image runs for shop.internal is replaced by host entries.
		"--add-host", "payments.shop.internal:127.0.0.1",
		"--add-host", "api.shop.internal:127.0.0.1",
		"--add-host", "partners.shop.internal:127.0.0.1",
	)
}

// cgroupV2 reports whether the Docker host runs cgroup v2. It assumes so
// when Docker doesn't answer; the docker run that follows reports that.
func cgroupV2() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := run(ctx, nil, "docker", "info", "--format", "{{.CgroupVersion}}")
	return err != nil || strings.TrimSpace(res.Stdout) != "1"
}

// EnsureNetwork creates the shared Docker network if it does not exist.
func EnsureNetwork(ctx context.Context) error {
	if res, err := run(ctx, nil, "docker", "network", "inspect", Network); err == nil && res.ExitCode == 0 {
		return nil
	}
	_, err := must(ctx, "docker", "network", "create", Network)
	return err
}

func (c *Container) Create(ctx context.Context, image string) error {
	if err := EnsureNetwork(ctx); err != nil {
		return err
	}
	args := append(RunArgs(SessionName),
		"--network", Network, "--network-alias", "scenario-vm",
		"-p", "127.0.0.1:18080:80",
		BaseImage(image))
	if _, err := must(ctx, "docker", args...); err != nil {
		return err
	}
	return waitBooted(ctx, c, 3*time.Minute)
}

func (c *Container) Exists(ctx context.Context) (bool, error) {
	res, err := run(ctx, nil, "docker", "container", "inspect", SessionName)
	return err == nil && res.ExitCode == 0, err
}

func (c *Container) Run(ctx context.Context, script string, env []string) (Result, error) {
	return run(ctx, strings.NewReader(envScript(script, env)), "docker", "exec", "-i", SessionName, "bash", "-s")
}

func (c *Container) CopyIn(ctx context.Context, local, remote string) error {
	// A tar through docker exec, since docker cp can't write into the
	// container's tmpfs mounts, such as /run.
	var buf bytes.Buffer
	if err := tarTree(&buf, local, remote); err != nil {
		return err
	}
	res, err := run(ctx, &buf, "docker", "exec", "-i", SessionName, "tar", "-x", "--no-overwrite-dir", "-C", "/", "-f", "-")
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("copy into machine: %s", strings.TrimSpace(res.Stderr))
	}
	return nil
}

func (c *Container) ShellCommand() []string {
	return []string{"docker", "exec", "-it", "-w", "/root", SessionName, "bash", "-l"}
}

func (c *Container) Reboot(ctx context.Context) error {
	if _, err := must(ctx, "docker", "restart", "-t", "30", SessionName); err != nil {
		return err
	}
	if err := waitBooted(ctx, c, 3*time.Minute); err != nil {
		return fmt.Errorf("after reboot: %w", err)
	}
	return nil
}

func (c *Container) Delete(ctx context.Context) error {
	_, err := must(ctx, "docker", "rm", "-f", SessionName)
	return err
}

// HostOnNetwork returns the host's address on the shared Docker network,
// where the CLI serves the metrics Prometheus scrapes. It returns "" under
// Docker Desktop, whose network gateway is inside Desktop's own VM; the
// telemetry stack scrapes host.docker.internal there, which reaches the
// CLI's loopback.
func HostOnNetwork(ctx context.Context) (string, error) {
	if dockerDesktop(ctx) {
		return "", EnsureNetwork(ctx)
	}
	return NetworkGateway(ctx)
}

// dockerDesktop reports whether Docker is Docker Desktop, including its
// WSL2 integration.
func dockerDesktop(ctx context.Context) bool {
	res, err := run(ctx, nil, "docker", "info", "--format", "{{.OperatingSystem}}")
	return err == nil && strings.Contains(res.Stdout, "Docker Desktop")
}

// NetworkGateway returns the host's address on the shared Docker network.
func NetworkGateway(ctx context.Context) (string, error) {
	if err := EnsureNetwork(ctx); err != nil {
		return "", err
	}
	out, err := must(ctx, "docker", "network", "inspect", Network, "-f", "{{(index .IPAM.Config 0).Gateway}}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
