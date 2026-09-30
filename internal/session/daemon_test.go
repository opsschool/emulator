package session

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opsschool/simulator/internal/checks"
	"github.com/opsschool/simulator/internal/loadgen"
	"github.com/opsschool/simulator/internal/results"
	"github.com/opsschool/simulator/internal/scenario"
	"github.com/opsschool/simulator/internal/vm"
)

// fakeMachine records scripts and answers every Run with exit code 0.
type fakeMachine struct {
	mu      sync.Mutex
	scripts []string
}

func (f *fakeMachine) Name() string                                    { return "fake" }
func (f *fakeMachine) BaseReady(context.Context, string) (bool, error) { return true, nil }
func (f *fakeMachine) Create(context.Context, string) error            { return nil }
func (f *fakeMachine) Exists(context.Context) (bool, error)            { return true, nil }
func (f *fakeMachine) CopyIn(context.Context, string, string) error    { return nil }
func (f *fakeMachine) ShellCommand() []string                          { return nil }
func (f *fakeMachine) Reboot(context.Context) error                    { return nil }
func (f *fakeMachine) Delete(context.Context) error                    { return nil }
func (f *fakeMachine) TelemetryNetwork() string                        { return "" }
func (f *fakeMachine) Run(_ context.Context, script string, _ []string) (vm.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts = append(f.scripts, script)
	return vm.Result{}, nil
}

func TestDaemonAPI(t *testing.T) {
	// A shop whose health endpoint works, and a Prometheus that always
	// returns one sample.
	shop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer shop.Close()
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[0,"0"]}]}}`))
	}))
	defer prom.Close()

	s := &scenario.Scenario{
		Spec: scenario.Spec{ID: "linux-test", Level: 1},
		Checks: scenario.Checks{
			Mitigated: []scenario.Check{{Type: scenario.CheckHTTP, URL: shop.URL, ExpectStatus: 200}},
			Fixed:     []scenario.Check{{Type: scenario.CheckPromQL, Expr: "up < 2"}},
		},
		Hints: []string{"look at df", "look at the log level"},
	}
	s.Spec.MitigateHold.Duration = 0
	s.Spec.FixVerification.Restart = []string{"shop.service"}

	home := t.TempDir()
	st := &State{User: "jdoe", ScenarioID: "linux-test", StartedAt: time.Now(), TimeLimit: time.Hour}
	if err := Save(home, st); err != nil {
		t.Fatal(err)
	}
	m := &fakeMachine{}
	d := &Daemon{
		Home: home, Log: log.New(io.Discard, "", 0), Machine: m, Scenario: s,
		Engine: &checks.Engine{Scenario: s, Machine: m, Prometheus: prom.URL},
		Load:   loadgen.New(shop.URL, loadgen.Steady(0)),
	}
	ln := httptest.NewUnstartedServer(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Run the daemon's loop without its listener; drive the API directly.
	st, _ = Load(home)
	d.st = st
	d.stop = cancel
	d.setupMetrics()
	ln.Config.Handler = d.handler()
	ln.Start()
	defer ln.Close()

	d.tick(ctx) // hold is 0, so one passing tick passes mitigated
	if !d.st.Passed(results.TierMitigated) {
		t.Fatal("mitigated should have passed")
	}

	post := func(path string) string {
		resp, err := http.Post(ln.URL+path, "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	if h := post("/hint"); !strings.Contains(h, "look at df") {
		t.Errorf("first hint: %s", h)
	}
	post("/hint")
	if h := post("/hint"); !strings.Contains(h, `"hint":""`) {
		t.Errorf("hints should be exhausted: %s", h)
	}

	d.Load.SetProfile(loadgen.Steady(0))
	s.Spec.FixVerification.LoadReplay.Duration = 0
	v := &Verifier{Scenario: s, Machine: m, Engine: d.Engine, Load: d.Load, Settle: time.Millisecond}
	rep, err := v.Run(ctx, func(string) {})
	if err != nil || !rep.Pass {
		t.Fatalf("verify: %+v %v", rep, err)
	}
	if len(m.scripts) == 0 || !strings.Contains(m.scripts[0], "systemctl restart shop.service") {
		t.Errorf("restart not run: %q", m.scripts)
	}

	if out := post("/stop"); !strings.Contains(out, `"score":80`) {
		t.Errorf("stop result: %s", out)
	}
	rs, _ := results.Open(home).All()
	if len(rs) != 1 || rs[0].HintsUsed != 2 {
		t.Errorf("results: %+v", rs)
	}
}
