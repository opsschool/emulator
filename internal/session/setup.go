package session

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/opsschool/emulator/internal/checks"
	"github.com/opsschool/emulator/internal/loadgen"
	"github.com/opsschool/emulator/internal/scenario"
	"github.com/opsschool/emulator/internal/telemetry"
	"github.com/opsschool/emulator/internal/vm"
)

// Env holds what a session needs from the host.
type Env struct {
	Home    string
	Machine vm.Driver
	Say     func(string) // progress messages
}

// NewSeed returns a random session seed.
func NewSeed() uint64 {
	var b [8]byte
	rand.Read(b[:])
	return binary.LittleEndian.Uint64(b[:]) >> 1 // keep it positive in every language
}

// Stack returns the telemetry stack for this machine driver.
func (e *Env) Stack(ctx context.Context) (*telemetry.Stack, error) {
	s := telemetry.NewStack(filepath.Join(Dir(e.Home), "telemetry"))
	if n := e.Machine.TelemetryNetwork(); n != "" {
		gw, err := vm.NetworkGateway(ctx)
		if err != nil {
			return nil, err
		}
		s.UseNetwork(n, gw)
	}
	return s, nil
}

// Engine builds the check engine for a session.
func Engine(s *scenario.Scenario, m vm.Driver, st *State) *checks.Engine {
	return &checks.Engine{
		Scenario:   s,
		Machine:    m,
		Prometheus: telemetry.PrometheusURL(),
		Template: scenario.TemplateData(map[string]string{
			"vm":         fmt.Sprintf("127.0.0.1:%d", telemetry.ShopPort),
			"vm_admin":   "127.0.0.1:19091",
			"prometheus": telemetry.PrometheusURL(),
		}, st.Vars),
		Env: scenario.Env(st.ScenarioID, st.User, st.Seed, st.Vars),
	}
}

// LoadProfile returns the scenario's load profile.
func LoadProfile(s *scenario.Scenario) (loadgen.Profile, error) {
	var steps []loadgen.Step
	for _, st := range s.Spec.Load.Schedule {
		steps = append(steps, loadgen.Step{RPS: st.RPS, Duration: st.Duration.Duration})
	}
	return loadgen.ProfileByName(s.Spec.Load.Profile, steps)
}

// NewGenerator returns a load generator aimed at the scenario machine.
func NewGenerator(s *scenario.Scenario) (*loadgen.Generator, error) {
	p, err := LoadProfile(s)
	if err != nil {
		return nil, err
	}
	return loadgen.New(fmt.Sprintf("http://127.0.0.1:%d", telemetry.ShopPort), p), nil
}

// Bring up the telemetry stack and a fresh machine for the scenario.
func (e *Env) Bring(ctx context.Context, s *scenario.Scenario) error {
	ready, err := e.Machine.BaseReady(ctx, s.Spec.Image)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("the %s image is not built yet; run `opsschool image build %s --driver %s` (takes 10-20 minutes, once)",
			s.Spec.Image, s.Spec.Image, e.Machine.Name())
	}
	if exists, _ := e.Machine.Exists(ctx); exists {
		return fmt.Errorf("a scenario machine already exists; run `opsschool stop` first")
	}
	stack, err := e.Stack(ctx)
	if err != nil {
		return err
	}
	dash, err := telemetry.Dashboard(s.Spec.ID, s.Dashboard)
	if err != nil {
		return err
	}
	if n := e.Machine.TelemetryNetwork(); n != "" {
		if err := vm.EnsureNetwork(ctx); err != nil {
			return err
		}
	}
	// Boot first, so the dashboards start with a running machine rather
	// than its boot, which would skew the first minutes of every rate().
	e.Say("Booting the scenario machine")
	if err := e.Machine.Create(ctx, s.Spec.Image); err != nil {
		return err
	}
	if _, err := e.Machine.Run(ctx, settleScript, nil); err != nil {
		return err
	}
	e.Say("Starting telemetry (Prometheus, Loki, Grafana)")
	if err := stack.Render(dash); err != nil {
		return err
	}
	return stack.Up(ctx)
}

// settleScript waits until the machine's CPUs are at least 80% idle over
// five seconds (iowait counts as busy), for at most 90 seconds: the end of
// boot warms caches for a while after systemd reports it is up.
const settleScript = `
for _ in $(seq 18); do
  read -r _ u n s i w q sq st _ </proc/stat
  sleep 5
  read -r _ u2 n2 s2 i2 w2 q2 sq2 st2 _ </proc/stat
  total=$(( (u2+n2+s2+i2+w2+q2+sq2+st2) - (u+n+s+i+w+q+sq+st) ))
  if (( total > 0 && (i2 - i) * 100 / total >= 80 )); then exit 0; fi
done`

// Baseline is how long a session runs healthy, with load and telemetry,
// before the break: the dashboards show normal behavior to compare against.
const Baseline = 2 * time.Minute

// RunScript copies one of the scenario's scripts into the machine, runs it
// as root with the session environment, and removes it.
func RunScript(ctx context.Context, m vm.Driver, s *scenario.Scenario, name string, env []string) error {
	remote := "/var/lib/opsschool/run/" + filepath.Base(name)
	if err := m.CopyIn(ctx, s.Path(name), remote); err != nil {
		return err
	}
	res, err := m.Run(ctx, "bash "+remote+"; rc=$?; rm -f "+remote+"; exit $rc", env)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		out := strings.TrimSpace(res.Stderr + "\n" + res.Stdout)
		return fmt.Errorf("%s failed (exit %d):\n%s", name, res.ExitCode, out)
	}
	return nil
}

// Break records the preserve baselines and applies the scenario's fault.
func Break(ctx context.Context, eng *checks.Engine, m vm.Driver, s *scenario.Scenario) error {
	for _, c := range s.Checks.Preserve {
		if o := eng.Evaluate(ctx, c, checks.PhaseBaseline); !o.Pass {
			return fmt.Errorf("recording baseline for %s: %s", c.Label(), o.Detail)
		}
	}
	return RunScript(ctx, m, s, scenario.FileBreak, eng.Env)
}

// Teardown removes the machine and stops the telemetry stack. It keeps
// going after errors and returns the first.
func (e *Env) Teardown(ctx context.Context) error {
	var first error
	keep := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	if exists, _ := e.Machine.Exists(ctx); exists {
		e.Say("Deleting the scenario machine")
		keep(e.Machine.Delete(ctx))
	}
	e.Say("Stopping telemetry")
	if stack, err := e.Stack(ctx); err == nil {
		keep(stack.Down(ctx))
	}
	return first
}

// WaitForShop waits until the shop answers through the proxy.
func WaitForShop(ctx context.Context, timeout time.Duration) error {
	return telemetry.WaitHTTP(ctx, fmt.Sprintf("http://127.0.0.1:%d/health", telemetry.ShopPort), timeout)
}
