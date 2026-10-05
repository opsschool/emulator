package session

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/opsschool/emulator/internal/checks"
	"github.com/opsschool/emulator/internal/edge"
	"github.com/opsschool/emulator/internal/scenario"
	"github.com/opsschool/emulator/internal/telemetry"
	"github.com/opsschool/emulator/internal/vm"
)

// TestReport is the outcome of verifying a scenario end to end.
type TestReport struct {
	Steps  []TestStep
	Passed bool
}

// TestStep is one assertion.
type TestStep struct {
	Name   string
	Pass   bool
	Detail string
}

// TestScenario runs the CI verification from docs/design.md, "CI and
// linting": break must fail both tiers, mitigate.sh must pass mitigated but
// not fixed, and solve.sh must pass everything after fix verification. The
// telemetry stack and a fresh machine are brought up and torn down here.
func (e *Env) TestScenario(ctx context.Context, s *scenario.Scenario, seed uint64) (rep TestReport, err error) {
	st := &State{User: "ci", ScenarioID: s.Spec.ID, Seed: seed, Vars: scenario.ResolveVars(s.Spec.Randomize, seed)}
	step := func(name string, pass bool, detail string) bool {
		rep.Steps = append(rep.Steps, TestStep{name, pass, detail})
		mark := "ok  "
		if !pass {
			mark = "FAIL"
		}
		e.Say(fmt.Sprintf("%s %s %s", mark, name, detail))
		return pass
	}
	defer func() { rep.Passed = err == nil && allPass(rep.Steps) }()

	// Quiz answers must resolve for several seeds.
	quizOK := true
	for _, sd := range []uint64{seed, seed + 1, seed + 2} {
		if err := scenario.CheckQuizVars(s.Questions, scenario.ResolveVars(s.Spec.Randomize, sd)); err != nil {
			quizOK = step("quiz answers resolve for seed "+fmt.Sprint(sd), false, err.Error())
		}
	}
	if quizOK && s.HasQuiz() {
		step("quiz answers resolve for 3 seeds", true, "")
	}

	defer e.Teardown(context.Background())
	if err := e.Bring(ctx, s); err != nil {
		return rep, err
	}

	eng := Engine(s, e.Machine, st)
	gen, err := NewGenerator(s)
	if err != nil {
		return rep, err
	}
	lctx, stopLoad := context.WithCancel(ctx)
	defer stopLoad()
	// No session daemon runs here, so serve the edge metrics it would.
	ed := edge.New(fmt.Sprintf("http://127.0.0.1:%d", telemetry.LokiPort))
	stopEdge, err := serveEdge(lctx, e.Machine, ed)
	if err != nil {
		return rep, err
	}
	defer stopEdge()
	gen.OnResult = ed.Observe
	go ed.Run(lctx)
	go gen.Run(lctx)

	e.Say("Applying break.sh")
	if err := Break(ctx, eng, e.Machine, s); err != nil {
		return rep, err
	}
	// Let rate() windows fill with post-break traffic.
	if err := sleep(ctx, 75*time.Second); err != nil {
		return rep, err
	}
	mit, mo := eng.EvaluateAll(ctx, s.Checks.Mitigated, checks.PhaseCheck)
	step("after break: mitigated fails", !mit, summarize(mo))
	fix, fo := eng.EvaluateAll(ctx, s.Checks.Fixed, checks.PhaseCheck)
	step("after break: fixed fails", !fix, summarize(fo))

	e.Say("Applying mitigate.sh")
	if err := RunScript(ctx, e.Machine, s, scenario.FileMitigate, eng.Env); err != nil {
		return rep, err
	}
	held, detail := e.waitHold(ctx, eng, s)
	step("after mitigate: mitigated passes (held "+s.Spec.MitigateHold.Duration.String()+")", held, detail)
	fix, fo = eng.EvaluateAll(ctx, s.Checks.Fixed, checks.PhaseCheck)
	step("after mitigate: fixed still fails", !fix, summarize(fo))

	e.Say("Applying solve.sh")
	if err := RunScript(ctx, e.Machine, s, scenario.FileSolve, eng.Env); err != nil {
		return rep, err
	}
	v := &Verifier{Scenario: s, Machine: e.Machine, Engine: eng, Load: gen}
	vr, err := v.Run(ctx, e.Say)
	if err != nil {
		return rep, err
	}
	step("after solve: fix verification passes", vr.Pass, joinFailures(vr.Details))
	return rep, nil
}

// waitHold waits until the mitigated checks pass continuously for the hold
// period, giving up after the hold plus three minutes.
func (e *Env) waitHold(ctx context.Context, eng *checks.Engine, s *scenario.Scenario) (bool, string) {
	hold := s.Spec.MitigateHold.Duration
	deadline := time.Now().Add(hold + 3*time.Minute)
	var since time.Time
	var last []checks.Outcome
	for time.Now().Before(deadline) {
		ok, outs := eng.EvaluateAll(ctx, s.Checks.Mitigated, checks.PhaseCheck)
		last = outs
		switch {
		case !ok:
			since = time.Time{}
		case since.IsZero():
			since = time.Now()
		case time.Since(since) >= hold:
			return true, ""
		}
		if sleep(ctx, 5*time.Second) != nil {
			break
		}
	}
	return false, summarize(last)
}

func summarize(os []checks.Outcome) string {
	s := ""
	for _, o := range os {
		mark := "fail"
		if o.Pass {
			mark = "pass"
		}
		s += fmt.Sprintf("\n       %s %s (%s)", mark, o.Check.Label(), o.Detail)
	}
	return s
}

func joinFailures(fs []string) string {
	s := ""
	for _, f := range fs {
		s += "\n       " + f
	}
	return s
}

func allPass(ss []TestStep) bool {
	for _, s := range ss {
		if !s.Pass {
			return false
		}
	}
	return len(ss) > 0
}

// serveEdge serves the edge metrics where Prometheus scrapes the session
// daemon, for runs without one.
func serveEdge(ctx context.Context, m vm.Driver, ed *edge.Edge) (func(), error) {
	addrs := []string{ControlAddr}
	if m.TelemetryNetwork() != "" {
		host, err := vm.HostOnNetwork(ctx)
		if err != nil {
			return nil, err
		}
		if host != "" {
			addrs = append(addrs, fmt.Sprintf("%s:%d", host, telemetry.CLIMetricsPort))
		}
	}
	mux := http.NewServeMux()
	mux.Handle("GET /edge/metrics", promhttp.HandlerFor(ed.Registry, promhttp.HandlerOpts{}))
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	for _, a := range addrs {
		ln, err := net.Listen("tcp", a)
		if err != nil {
			srv.Close()
			return nil, fmt.Errorf("listen %s: %w", a, err)
		}
		go srv.Serve(ln)
	}
	return func() { srv.Close() }, nil
}
