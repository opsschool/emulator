package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/opsschool/emulator/internal/scenario"
	"github.com/opsschool/emulator/internal/session"
	"github.com/opsschool/emulator/internal/vm"
)

func init() {
	register("test", "test <path> [--driver d] [--seed n]", "Run a scenario's full CI verification locally.", runTest)
	register("image", "image build <image> [--driver d]", "Build a base machine image (once, 10-20 minutes).", runImage)
}

func runTest(e *Env, args []string) error {
	fs := newFlags(e, "test")
	seed := fs.Uint64("seed", 1, "session seed")
	drv := driverFlag(fs)
	dir, rest := splitFirst(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if dir == "" || fs.NArg() != 0 {
		return errUsage
	}
	s, ps := scenario.Validate(dir)
	for _, p := range ps {
		fmt.Fprintln(e.Stdout, p)
	}
	if scenario.HasErrors(ps) {
		return exitError{1}
	}
	home, err := Home(e)
	if err != nil {
		return err
	}
	if _, err := session.Load(home); err == nil {
		return errors.New("a scenario session is running; `opsschool stop` it first")
	}
	m, err := vm.New(firstNonEmpty(*drv, e.Getenv("OPSSCHOOL_DRIVER")))
	if err != nil {
		return err
	}
	ctx, cancel := signalContext()
	defer cancel()
	env := &session.Env{Home: home, Machine: m, Say: func(s string) { fmt.Fprintln(e.Stdout, "==> "+s) }}
	rep, err := env.TestScenario(ctx, s, *seed)
	if err != nil {
		return err
	}
	if !rep.Passed {
		fmt.Fprintf(e.Stdout, "\n%s: FAILED\n", s.Spec.ID)
		return exitError{1}
	}
	fmt.Fprintf(e.Stdout, "\n%s: passed\n", s.Spec.ID)
	return nil
}

func runImage(e *Env, args []string) error {
	if len(args) < 2 || args[0] != "build" {
		return errUsage
	}
	image := args[1]
	fs := newFlags(e, "image build")
	drv := driverFlag(fs)
	orders := fs.Int("orders", 0, "override the seeded order count (for quick local builds)")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	m, err := vm.New(firstNonEmpty(*drv, e.Getenv("OPSSCHOOL_DRIVER")))
	if err != nil {
		return err
	}
	root, err := repoRoot(e)
	if err != nil {
		return err
	}
	imgDir := filepath.Join(root, "images", image)
	if _, err := os.Stat(filepath.Join(imgDir, "provision.sh")); err != nil {
		return fmt.Errorf("no image %q in %s", image, filepath.Join(root, "images"))
	}
	ctx, cancel := signalContext()
	defer cancel()
	switch m.Name() {
	case "lima":
		cmd := exec.CommandContext(ctx, filepath.Join(imgDir, "build.sh"))
		cmd.Dir = root
		cmd.Stdout, cmd.Stderr = e.Stdout, e.Stderr
		if *orders > 0 {
			cmd.Env = append(os.Environ(), fmt.Sprintf("SEED_ORDERS=%d", *orders))
		}
		return cmd.Run()
	default:
		return vm.BuildContainerBase(ctx, vm.BuildOptions{
			Root: root, Image: image, Arch: runtime.GOARCH, SeedOrders: *orders,
			Out: e.Stdout,
		})
	}
}

// repoRoot finds the emulator repository: the parent of the scenarios
// directory.
func repoRoot(e *Env) (string, error) {
	sc, err := scenariosRoot(e, "")
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Dir(sc))
}
