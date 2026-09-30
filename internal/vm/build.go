package vm

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
)

// BuildOptions configures a base image build.
type BuildOptions struct {
	Root       string // repository root
	Image      string // e.g. single-node
	Arch       string // amd64 or arm64
	SeedOrders int    // 0 keeps versions.env
	Out        io.Writer
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
	if err := step("copying the build", "docker", "cp", stage, builder+":/opt/opsschool-build"); err != nil {
		return err
	}
	if err := step("provisioning (several minutes)", "docker", "exec", "-e", "OPSSCHOOL_PROVISION=container", builder,
		"bash", "/opt/opsschool-build/provision.sh", "/opt/opsschool-build"); err != nil {
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
	return step("saving "+BaseImage(o.Image), "docker", "commit", builder, BaseImage(o.Image))
}
