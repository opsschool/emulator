package vm

import (
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
	return []string{
		"run", "-d", "--name", name, "--hostname", "scenario-vm",
		"--privileged", "--cgroupns=host", "-v", "/sys/fs/cgroup:/sys/fs/cgroup:rw",
		"--tmpfs", "/run", "--tmpfs", "/run/lock",
		// Docker owns /etc/resolv.conf, so the site resolver that the VM
		// image runs for shop.internal is replaced by host entries.
		"--add-host", "payments.shop.internal:127.0.0.1",
		"--add-host", "api.shop.internal:127.0.0.1",
		"--add-host", "partners.shop.internal:127.0.0.1",
	}
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
	res, err := c.Run(ctx, "mkdir -p \"$(dirname "+shellQuote(remote)+")\"", nil)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("copy into machine: %s", strings.TrimSpace(res.Stderr))
	}
	_, err = must(ctx, "docker", "cp", local, SessionName+":"+remote)
	return err
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
