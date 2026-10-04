package session

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opsschool/emulator/internal/checks"
	"github.com/opsschool/emulator/internal/loadgen"
	"github.com/opsschool/emulator/internal/results"
	"github.com/opsschool/emulator/internal/scenario"
	"github.com/opsschool/emulator/internal/vm"
)

// fakeMachine records scripts and answers every Run with exit code 0.
type fakeMachine struct {
	mu          sync.Mutex
	scripts     []string
	fingerprint string // what the base image was built from
}

func (f *fakeMachine) Name() string { return "fake" }
func (f *fakeMachine) Base(context.Context, string) (bool, string, error) {
	return true, f.fingerprint, nil
}
func (f *fakeMachine) Create(context.Context, string) error         { return nil }
func (f *fakeMachine) Exists(context.Context) (bool, error)         { return true, nil }
func (f *fakeMachine) CopyIn(context.Context, string, string) error { return nil }
func (f *fakeMachine) ShellCommand() []string                       { return nil }
func (f *fakeMachine) Reboot(context.Context) error                 { return nil }
func (f *fakeMachine) Delete(context.Context) error                 { return nil }
func (f *fakeMachine) TelemetryNetwork() string                     { return "" }
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
		Hints: []string{"look at df"},
	}
	s.Spec.Curriculum = "https://www.opsschool.org/filesystems_101.html"
	s.Spec.MitigateHold.Duration = 0
	s.Spec.FixVerification.Restart = []string{"shop.service"}

	home := t.TempDir()
	st := &State{User: "jdoe", ScenarioID: "linux-test", TimeLimit: time.Hour}
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

	post := func(path string) string {
		resp, err := http.Post(ln.URL+path, "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}

	// Before the scenario begins nothing is graded and hints are refused.
	d.tick(ctx)
	if d.st.Passed(results.TierMitigated) || d.st.Elapsed(time.Now()) != 0 {
		t.Fatal("graded before the scenario began")
	}
	if h := post("/hint"); !strings.Contains(h, "not begun") {
		t.Errorf("hint before begin: %s", h)
	}
	page := func() Page {
		resp, err := http.Get(ln.URL + "/status")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var st Status
		if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
			t.Fatal(err)
		}
		return st.Page
	}
	s.Spec.Alerts, s.Spec.Summary = []string{"ShopOrderErrors"}, "Orders are failing."
	if p := page(); p.Summary != "" || len(p.Alerts) != 0 || !p.HasDocs || p.Docs != "" || len(p.Hints) != 0 {
		t.Errorf("page before the scenario began shows too much: %+v", p)
	}
	post("/baseline")
	if d.st.BaselineEnds.Before(time.Now()) {
		t.Error("/baseline did not set when the baseline ends")
	}
	post("/begin")
	if !d.st.Begun() {
		t.Fatal("/begin did not start the scenario")
	}

	d.tick(ctx) // hold is 0, so one passing tick passes mitigated
	if !d.st.Passed(results.TierMitigated) {
		t.Fatal("mitigated should have passed")
	}
	// The curriculum comes first and is free; then the paid hint.
	if h := post("/hint"); !strings.Contains(h, "filesystems_101") || d.st.HintsUsed != 0 || !d.st.DocsHint {
		t.Errorf("first hint should be the free curriculum link: %s (used %d)", h, d.st.HintsUsed)
	}
	if h := post("/hint"); !strings.Contains(h, "look at df") {
		t.Errorf("second hint: %s", h)
	}
	if h := post("/hint"); !strings.Contains(h, `"hint":""`) {
		t.Errorf("hints should be exhausted: %s", h)
	}
	if p := page(); p.Summary == "" || len(p.Alerts) != 1 || p.Docs == "" || len(p.Hints) != 1 || p.Hints[0] != "look at df" {
		t.Errorf("page after the hints: %+v", p)
	}
	// Another site can't drive the session from the learner's browser.
	req, _ := http.NewRequest(http.MethodPost, ln.URL+"/hint", nil)
	req.Header.Set("Origin", "https://evil.example")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin hint: %v %v", resp, err)
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

	if out := post("/stop"); !strings.Contains(out, `"score":90`) {
		t.Errorf("stop result: %s", out)
	}
	rs, _ := results.Open(home).All()
	if len(rs) != 1 || rs[0].HintsUsed != 1 || !rs[0].DocsHint {
		t.Errorf("results: %+v", rs)
	}
}

// Learners see which checks failed, not their output, which can name the cause.
func TestVerifyReportHidesDetails(t *testing.T) {
	var r VerifyReport
	r.fail("fixed: the cause is fixed", "no cron job cleans up /data/sessions")
	if strings.Contains(strings.Join(r.Failures, ""), "cron") {
		t.Errorf("failures leak the detail: %q", r.Failures)
	}
	if !strings.Contains(strings.Join(r.Details, ""), "cron") {
		t.Errorf("details lost: %q", r.Details)
	}
}

func TestVerifyKeepsMitigatedHold(t *testing.T) {
	s := &scenario.Scenario{Spec: scenario.Spec{ID: "linux-test"}}
	s.Spec.MitigateHold.Duration = time.Minute
	newDaemon := func(since time.Time) *Daemon {
		d := &Daemon{Home: t.TempDir(), Log: log.New(io.Discard, "", 0), Scenario: s, since: since}
		d.st = &State{ScenarioID: "linux-test", StartedAt: since.Add(-time.Minute)}
		d.setupMetrics()
		return d
	}
	now := time.Now()
	since := now.Add(-90 * time.Second)

	// Fix not verified, but the service stayed mitigated: the hold carries on.
	d := newDaemon(since)
	d.finishVerify(&VerifyReport{MitigatedPass: true}, now)
	if !d.since.Equal(since) {
		t.Errorf("hold restarted after a verification that stayed mitigated")
	}

	// Mitigated checks failed at the end of verification: the hold restarts.
	d = newDaemon(since)
	d.finishVerify(&VerifyReport{}, now)
	if !d.since.IsZero() {
		t.Errorf("hold kept after mitigated checks failed")
	}

	// A passing verification credits mitigated when the hold completed.
	d = newDaemon(since)
	d.finishVerify(&VerifyReport{Pass: true, MitigatedPass: true}, now)
	if got, want := d.st.TierPassed[results.TierMitigated], since.Add(time.Minute); !got.Equal(want) {
		t.Errorf("mitigated passed at %s, want %s", got, want)
	}
	if got := d.st.TierPassed[results.TierFixed]; !got.Equal(now) {
		t.Errorf("fixed passed at %s, want %s", got, now)
	}
}
