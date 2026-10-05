package vm

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// BuildOptions configures a base image build.
type BuildOptions struct {
	Root       string // repository root
	Image      string // e.g. single-node
	Arch       string // amd64 or arm64
	SeedOrders int    // 0 keeps versions.env
	// Fingerprint is recorded on the image; see Fingerprint.
	Fingerprint string
	Out         io.Writer
}

// FaultVariants are the alternate shop builds shipped in every image, by
// build tag. They are installed under /usr/local/lib/shop-builds/<name>.
var FaultVariants = []string{"faultconnleak", "faultmemleak", "faultidletx", "faultnobackoff"}

// StageBuild cross-compiles the shop and its fault variants and copies the
// provisioning files into a fresh directory, laid out as provision.sh
// expects.
func StageBuild(ctx context.Context, o BuildOptions) (string, error) {
	imgDir := filepath.Join(o.Root, "images", o.Image)
	env, err := os.ReadFile(filepath.Join(imgDir, "versions.env"))
	if err != nil {
		return "", err
	}
	version := "dev"
	if m := regexp.MustCompile(`(?m)^SHOP_VERSION=(\S+)`).FindSubmatch(env); m != nil {
		version = string(m[1])
	}
	if o.SeedOrders > 0 {
		env = regexp.MustCompile(`(?m)^SEED_ORDERS=.*$`).ReplaceAll(env, []byte(fmt.Sprintf("SEED_ORDERS=%d", o.SeedOrders)))
	}
	dir, err := os.MkdirTemp("", "opsschool-build-")
	if err != nil {
		return "", err
	}
	bin := filepath.Join(dir, "bin", o.Arch)
	build := func(out, tags, ver string) error {
		fmt.Fprintf(o.Out, "==> building %s\n", filepath.Base(out))
		cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-tags", tags,
			"-ldflags", "-s -w -X main.Version="+ver, "-o", out, "./demoapp/cmd/shop")
		cmd.Dir = o.Root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+o.Arch)
		cmd.Stdout, cmd.Stderr = o.Out, o.Out
		return cmd.Run()
	}
	if err := build(filepath.Join(bin, "shop"), "", version); err != nil {
		return "", err
	}
	for _, tag := range FaultVariants {
		// Faulty builds are versioned as the next release, like a bad deploy.
		if err := build(filepath.Join(bin, "shop-"+tag[len("fault"):]), tag, "2.4.0"); err != nil {
			return "", err
		}
	}
	for _, f := range []string{"files", "provision.sh", "smoke.sh"} {
		if err := exec.CommandContext(ctx, "cp", "-r", filepath.Join(imgDir, f), dir).Run(); err != nil {
			return "", fmt.Errorf("staging %s: %w", f, err)
		}
	}
	return dir, os.WriteFile(filepath.Join(dir, "versions.env"), env, 0o644)
}

// BuildContainerBase builds the base image for the container driver: a
// systemd container provisioned by the image's provision.sh, committed as
// BaseImage(image).
func BuildContainerBase(ctx context.Context, o BuildOptions) error {
	stage, err := StageBuild(ctx, o)
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	imgDir := filepath.Join(o.Root, "images", o.Image)
	sys := "opsschool/" + o.Image + ":systemd"
	builder := "opsschool-build-" + o.Image
	step := func(msg string, name string, args ...string) error {
		fmt.Fprintf(o.Out, "==> %s\n", msg)
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Stdout, cmd.Stderr = o.Out, o.Out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w", msg, err)
		}
		return nil
	}
	run(ctx, nil, "docker", "rm", "-f", builder)
	defer run(context.Background(), nil, "docker", "rm", "-f", builder)
	if err := step("building the systemd image", "docker", "build", "-t", sys, "-f",
		filepath.Join(imgDir, "container", "Dockerfile"), filepath.Join(imgDir, "container")); err != nil {
		return err
	}
	if err := step("booting the builder", "docker", append(RunArgs(builder), sys)...); err != nil {
		return err
	}
	// Provisioning while systemd is still booting races with its /tmp
	// cleanup, which breaks apt's signature checks.
	if err := step("waiting for boot", "docker", "exec", builder, "bash", "-c",
		"systemctl is-system-running --wait >/dev/null || true"); err != nil {
		return err
	}
	if err := step("copying the build", "docker", "cp", stage, builder+":/opt/opsschool-build"); err != nil {
		return err
	}
	if err := step("provisioning (several minutes)", "docker", "exec", "-e", "OPSSCHOOL_PROVISION=container", builder,
		"bash", "/opt/opsschool-build/provision.sh", "/opt/opsschool-build"); err != nil {
		// The builder is deleted on return, so show the failed units' logs.
		step("logs of failed units", "docker", "exec", builder, "bash", "-c",
			"for u in $(systemctl --failed --no-legend --plain | awk '{print $1}'); do journalctl -u \"$u\" --no-pager -n 40; done")
		return err
	}
	if err := step("smoke test", "docker", "exec", builder, "bash", "/opt/opsschool-build/smoke.sh"); err != nil {
		return err
	}
	if err := step("cleaning up", "docker", "exec", builder, "bash", "-c",
		"rm -rf /opt/opsschool-build; truncate -s 0 /data/log/shop/*.log; journalctl --rotate --vacuum-time=1s >/dev/null 2>&1 || true"); err != nil {
		return err
	}
	if err := step("stopping", "docker", "stop", "-t", "60", builder); err != nil {
		return err
	}
	return step("saving "+BaseImage(o.Image), "docker", "commit",
		"--change", "LABEL "+FingerprintLabel+"="+o.Fingerprint, builder, BaseImage(o.Image))
}

// VMDiskImage names the container disk image that KubeVirt boots for an
// image name: the Lima base's disk, packaged as KubeVirt expects.
func VMDiskImage(image string) string { return "opsschool/" + image + "-vm:base" }

// BuildVMDisk packages the Lima base as a KubeVirt container disk. It
// needs a current Lima base: it copies a clone of it, cleaned of Lima's
// agent, user and network config by images/vm-export.sh, so the same
// machine boots under Lima on a laptop and KubeVirt in hosted mode.
func BuildVMDisk(ctx context.Context, o BuildOptions) error {
	l := &Lima{}
	built, fp, err := l.Base(ctx, o.Image)
	if err != nil {
		return err
	}
	if !built {
		return fmt.Errorf("build the Lima base first: opsschool image build %s --driver lima", o.Image)
	}
	if fp != o.Fingerprint {
		return fmt.Errorf("the Lima base is out of date with this checkout; rebuild it first: opsschool image build %s --driver lima", o.Image)
	}
	step := func(msg string, name string, args ...string) error {
		fmt.Fprintf(o.Out, "==> %s\n", msg)
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Stdout, cmd.Stderr = o.Out, o.Out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w", msg, err)
		}
		return nil
	}
	clone := "opsschool-export-" + o.Image
	run(ctx, nil, "limactl", "delete", "-f", clone)
	defer run(context.Background(), nil, "limactl", "delete", "-f", clone)
	if err := step("cloning the Lima base", "limactl", "clone", "--tty=false", BaseInstance(o.Image), clone); err != nil {
		return err
	}
	if err := step("booting the clone", "limactl", "start", "--tty=false", clone); err != nil {
		return err
	}
	user, err := must(ctx, "limactl", "shell", clone, "id", "-un")
	if err != nil {
		return err
	}
	if err := step("copying the export script", "limactl", "copy", filepath.Join(o.Root, "images", "vm-export.sh"), clone+":/tmp/vm-export.sh"); err != nil {
		return err
	}
	if err := step("removing Lima's agent, user and network config", "limactl", "shell", clone,
		"sudo", "bash", "/tmp/vm-export.sh", strings.TrimSpace(user)); err != nil {
		return err
	}
	fmt.Fprintln(o.Out, "==> waiting for the clone to power off")
	deadline := time.Now().Add(3 * time.Minute)
	for {
		out, err := must(ctx, "limactl", "list", "--format", "{{.Status}}", clone)
		if err == nil && strings.TrimSpace(out) == "Stopped" {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the clone did not power off")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	dir, err := must(ctx, "limactl", "list", "--format", "{{.Dir}}", clone)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "opsschool-vmdisk-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	// Lima 2 calls the disk "disk", older releases "diffdisk" (copy on
	// write over "basedisk"). Converting flattens either one.
	disk := filepath.Join(strings.TrimSpace(dir), "disk")
	if _, err := os.Stat(disk); err != nil {
		disk = filepath.Join(strings.TrimSpace(dir), "diffdisk")
	}
	if err := step("copying the disk (a few minutes)", "qemu-img", "convert", "-c", "-O", "qcow2",
		disk, filepath.Join(stage, "disk.qcow2")); err != nil {
		return err
	}
	// KubeVirt's container disk format: the image under /disk, owned by
	// the qemu user (107).
	dockerfile := "FROM scratch\nADD --chown=107:107 disk.qcow2 /disk/\nLABEL " + FingerprintLabel + "=" + o.Fingerprint + "\n"
	if err := os.WriteFile(filepath.Join(stage, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return err
	}
	return step("saving "+VMDiskImage(o.Image), "docker", "build", "-t", VMDiskImage(o.Image), stage)
}
