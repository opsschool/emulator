// Package vm runs scenario machines. Lima runs real VMs; the container
// driver runs a privileged systemd container, for development and CI where
// KVM is not available.
package vm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// SessionName is the machine name of the running scenario. The MVP runs one
// scenario at a time.
const SessionName = "opsschool-session"

// Result of running a command in the machine.
type Result struct {
	Stdout, Stderr string
	ExitCode       int
}

// Driver manages scenario machines.
type Driver interface {
	// Name is "lima" or "container".
	Name() string
	// Base reports whether the base image for an image name is built, and
	// the Fingerprint it was built from ("" for builds that predate them).
	Base(ctx context.Context, image string) (built bool, fingerprint string, err error)
	// Create makes a fresh session machine from the base image and boots it.
	Create(ctx context.Context, image string) error
	// Exists reports whether the session machine exists.
	Exists(ctx context.Context) (bool, error)
	// Run runs a bash script as root in the session machine with extra
	// environment variables. A non-zero exit is not an error.
	Run(ctx context.Context, script string, env []string) (Result, error)
	// CopyIn copies a local file or directory into the machine.
	CopyIn(ctx context.Context, local, remote string) error
	// ShellCommand returns an interactive root shell command for the learner.
	ShellCommand() []string
	// Reboot restarts the machine and waits for it to boot.
	Reboot(ctx context.Context) error
	// Delete removes the session machine.
	Delete(ctx context.Context) error
	// TelemetryNetwork is the Docker network the telemetry stack must join
	// to reach the machine, or "" when it is reached through host ports.
	TelemetryNetwork() string
}

// New returns the named driver: "lima", "container", or "" to pick Lima
// when limactl is installed and the container driver otherwise.
func New(name string) (Driver, error) {
	switch name {
	case "":
		if _, err := exec.LookPath("limactl"); err == nil {
			return &Lima{}, nil
		}
		if _, err := exec.LookPath("docker"); err == nil {
			return &Container{}, nil
		}
		return nil, errors.New("install Lima (https://lima-vm.io) to run scenarios")
	case "lima":
		return &Lima{}, nil
	case "container":
		return &Container{}, nil
	}
	return nil, fmt.Errorf("unknown driver %q (lima or container)", name)
}

// run runs a host command and returns its output.
func run(ctx context.Context, stdin io.Reader, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errb strings.Builder
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, &out, &errb
	err := cmd.Run()
	res := Result{Stdout: out.String(), Stderr: errb.String()}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
		return res, nil
	}
	return res, err
}

// must runs a host command and fails on a non-zero exit.
func must(ctx context.Context, name string, args ...string) (string, error) {
	res, err := run(ctx, nil, name, args...)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("%s %s: exit %d: %s", name, strings.Join(args, " "), res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	return res.Stdout, nil
}

// envScript prefixes a script with exported environment variables.
func envScript(script string, env []string) string {
	var b strings.Builder
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		fmt.Fprintf(&b, "export %s=%s\n", k, shellQuote(v))
	}
	b.WriteString(script)
	return b.String()
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// waitBooted polls until systemd reports the machine is up.
func waitBooted(ctx context.Context, d Driver, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		res, err := d.Run(ctx, "systemctl is-system-running --wait >/dev/null 2>&1; systemctl is-active --quiet shop", nil)
		if err == nil && res.ExitCode == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			failed := ""
			if r, err := d.Run(ctx, "systemctl --failed --no-legend --plain | awk '{print $1}' | paste -sd' '", nil); err == nil {
				failed = strings.TrimSpace(r.Stdout)
			}
			if failed == "" {
				failed = "none; shop.service is not active"
			}
			return fmt.Errorf("machine did not finish booting within %s (failed units: %s)", timeout, failed)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// Interactive runs an interactive command attached to the terminal.
func Interactive(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
