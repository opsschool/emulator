package vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Lima runs scenario machines as Lima VMs. The base VM is built once by
// images/<image>/build.sh and left stopped; each session is a clone of it.
type Lima struct{}

// BaseInstance returns the Lima instance holding a built base machine.
func BaseInstance(image string) string { return "opsschool-" + image }

func (l *Lima) Name() string             { return "lima" }
func (l *Lima) TelemetryNetwork() string { return "" }

func (l *Lima) instances(ctx context.Context) ([]string, error) {
	out, err := must(ctx, "limactl", "list", "-q")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

func (l *Lima) has(ctx context.Context, name string) (bool, error) {
	names, err := l.instances(ctx)
	if err != nil {
		return false, err
	}
	for _, n := range names {
		if n == name {
			return true, nil
		}
	}
	return false, nil
}

// ReadyMarker is written into the base instance's directory by
// images/<image>/build.sh once the build has finished.
const ReadyMarker = "opsschool-ready"

func (l *Lima) BaseReady(ctx context.Context, image string) (bool, error) {
	ok, err := l.has(ctx, BaseInstance(image))
	if err != nil || !ok {
		return false, err
	}
	dir, err := must(ctx, "limactl", "list", "--format", "{{.Dir}}", BaseInstance(image))
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(strings.TrimSpace(dir), ReadyMarker))
	return err == nil, nil
}

func (l *Lima) Create(ctx context.Context, image string) error {
	if _, err := must(ctx, "limactl", "clone", BaseInstance(image), SessionName); err != nil {
		return err
	}
	if _, err := must(ctx, "limactl", "start", "--tty=false", SessionName); err != nil {
		return err
	}
	return waitBooted(ctx, l, 5*time.Minute)
}

func (l *Lima) Exists(ctx context.Context) (bool, error) { return l.has(ctx, SessionName) }

func (l *Lima) Run(ctx context.Context, script string, env []string) (Result, error) {
	return run(ctx, strings.NewReader(envScript(script, env)), "limactl", "shell", "--workdir", "/", SessionName, "sudo", "bash", "-s")
}

func (l *Lima) CopyIn(ctx context.Context, local, remote string) error {
	// limactl copy runs as the Lima user; stage in /tmp, then move as root.
	tmp := "/tmp/opsschool-copy-" + fmt.Sprint(time.Now().UnixNano())
	args := []string{"copy", local, SessionName + ":" + tmp}
	// Only for directories: the rsync backend treats a -r source as one.
	if fi, err := os.Stat(local); err != nil {
		return err
	} else if fi.IsDir() {
		args = append(args[:1], append([]string{"-r"}, args[1:]...)...)
	}
	if _, err := must(ctx, "limactl", args...); err != nil {
		return err
	}
	res, err := l.Run(ctx, fmt.Sprintf("rm -rf %s && mkdir -p \"$(dirname %s)\" && mv %s %s", shellQuote(remote), shellQuote(remote), tmp, shellQuote(remote)), nil)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("copy into VM: %s", res.Stderr)
	}
	return nil
}

func (l *Lima) ShellCommand() []string {
	return []string{"limactl", "shell", "--workdir", "/root", SessionName, "sudo", "-i"}
}

func (l *Lima) Reboot(ctx context.Context) error {
	if _, err := must(ctx, "limactl", "stop", SessionName); err != nil {
		return err
	}
	if _, err := must(ctx, "limactl", "start", "--tty=false", SessionName); err != nil {
		return err
	}
	return waitBooted(ctx, l, 5*time.Minute)
}

func (l *Lima) Delete(ctx context.Context) error {
	_, err := must(ctx, "limactl", "delete", "-f", SessionName)
	return err
}
