package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/opsschool/simulator/internal/checks"
	"github.com/opsschool/simulator/internal/loadgen"
	"github.com/opsschool/simulator/internal/results"
	"github.com/opsschool/simulator/internal/scenario"
	"github.com/opsschool/simulator/internal/telemetry"
	"github.com/opsschool/simulator/internal/vm"
)

// ControlAddr is where the daemon serves /metrics and its control API.
var ControlAddr = fmt.Sprintf("127.0.0.1:%d", telemetry.CLIMetricsPort)

// Daemon runs in the background for the whole session. It drives load,
// grades the mitigated tier continuously, exports the session metrics, and
// serves the control API the other commands use.
type Daemon struct {
	Home     string
	Log      *log.Logger
	Machine  vm.Driver
	Scenario *scenario.Scenario
	Engine   *checks.Engine
	Load     *loadgen.Generator
	// ExtraListen is another address to serve metrics on, so Prometheus
	// can reach it from a Docker network. Optional.
	ExtraListen string

	mu        sync.Mutex
	st        *State
	verifying bool
	since     time.Time // when the mitigated checks started passing
	lastMit   []checks.Outcome
	stop      context.CancelFunc

	reg      *prometheus.Registry
	passed   *prometheus.GaugeVec
	elapsed  prometheus.Gauge
	hints    prometheus.Gauge
	requests *prometheus.CounterVec
}

// Run runs the daemon until the session is stopped or ctx ends.
func (d *Daemon) Run(ctx context.Context) error {
	st, err := Load(d.Home)
	if err != nil {
		return err
	}
	d.st = st
	ctx, d.stop = context.WithCancel(ctx)
	defer d.stop()
	d.setupMetrics()

	srv := &http.Server{Handler: d.handler(), ReadHeaderTimeout: 5 * time.Second}
	addrs := []string{ControlAddr}
	if d.ExtraListen != "" {
		addrs = append(addrs, d.ExtraListen)
	}
	for _, a := range addrs {
		ln, err := net.Listen("tcp", a)
		if err != nil {
			return fmt.Errorf("listen %s: %w", a, err)
		}
		go srv.Serve(ln)
	}
	defer srv.Close()

	d.Load.OnResult = func(route string, code int) {
		d.requests.WithLabelValues(route, strconv.Itoa(code)).Inc()
	}
	go d.Load.Run(ctx)

	d.Log.Printf("session started: %s as %s", st.ScenarioID, st.User)
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		d.tick(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func (d *Daemon) setupMetrics() {
	d.reg = prometheus.NewRegistry()
	d.passed = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "opsschool_check_passed", Help: "1 when the tier has passed."}, []string{"scenario", "tier"})
	d.elapsed = prometheus.NewGauge(prometheus.GaugeOpts{Name: "opsschool_session_elapsed_seconds", Help: "Seconds since the session started."})
	d.hints = prometheus.NewGauge(prometheus.GaugeOpts{Name: "opsschool_hints_used", Help: "Hints revealed so far."})
	d.requests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "opsschool_loadgen_requests_total", Help: "Load generator requests by route and status code (0 = transport error)."}, []string{"route", "code"})
	d.reg.MustRegister(d.passed, d.elapsed, d.hints, d.requests)
	d.syncMetrics()
}

// syncMetrics copies state into the gauges. Call with d.mu held or before
// the daemon is serving.
func (d *Daemon) syncMetrics() {
	for _, t := range results.Tiers {
		v := 0.0
		if d.st.Passed(t) {
			v = 1
		}
		d.passed.WithLabelValues(d.st.ScenarioID, t).Set(v)
	}
	d.elapsed.Set(d.st.Elapsed(time.Now()).Seconds())
	d.hints.Set(float64(d.st.HintsUsed))
}

// tick evaluates the mitigated tier.
func (d *Daemon) tick(ctx context.Context) {
	d.mu.Lock()
	d.syncMetrics()
	skip := d.verifying || d.st.Passed(results.TierMitigated) || d.st.OverTime(time.Now())
	d.mu.Unlock()
	if skip {
		return
	}
	ok, outs := d.Engine.EvaluateAll(ctx, d.Scenario.Checks.Mitigated, checks.PhaseCheck)
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastMit = outs
	if !ok {
		d.since = time.Time{}
		return
	}
	if d.since.IsZero() {
		d.since = now
	}
	if now.Sub(d.since) >= d.Scenario.Spec.MitigateHold.Duration {
		d.passTier(results.TierMitigated, now)
	}
}

// passTier records a tier pass. Call with d.mu held.
func (d *Daemon) passTier(tier string, at time.Time) {
	if d.st.Passed(tier) {
		return
	}
	if d.st.TierPassed == nil {
		d.st.TierPassed = map[string]time.Time{}
	}
	d.st.TierPassed[tier] = at
	msg := fmt.Sprintf("Tier %s passed at %s.", tier, d.st.Elapsed(at))
	d.addEvent(msg)
	d.syncMetrics()
	notify("Ops School: "+d.st.ScenarioID, msg)
}

// addEvent records an event and saves state. Call with d.mu held.
func (d *Daemon) addEvent(msg string) {
	d.st.Events = append(d.st.Events, Event{At: time.Now(), Msg: msg})
	d.Log.Print(msg)
	d.save()
}

func (d *Daemon) save() {
	if err := Save(d.Home, d.st); err != nil {
		d.Log.Printf("saving state: %v", err)
	}
}

// Status is the reply to GET /status.
type Status struct {
	State     *State           `json:"state"`
	Elapsed   time.Duration    `json:"elapsed"`
	Verifying bool             `json:"verifying"`
	Mitigated []checks.Outcome `json:"mitigated_checks"`
	HoldLeft  time.Duration    `json:"hold_left"`
}

func (d *Daemon) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(d.reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		s := Status{State: d.st, Elapsed: d.st.Elapsed(time.Now()), Verifying: d.verifying, Mitigated: d.lastMit}
		if !d.since.IsZero() && !d.st.Passed(results.TierMitigated) {
			s.HoldLeft = max(0, d.Scenario.Spec.MitigateHold.Duration-time.Since(d.since)).Truncate(time.Second)
		}
		writeJSON(w, s)
	})
	mux.HandleFunc("POST /hint", func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.st.HintsUsed >= len(d.Scenario.Hints) {
			writeJSON(w, map[string]any{"hint": "", "index": d.st.HintsUsed, "total": len(d.Scenario.Hints)})
			return
		}
		h := d.Scenario.Hints[d.st.HintsUsed]
		d.st.HintsUsed++
		d.addEvent(fmt.Sprintf("Hint %d revealed.", d.st.HintsUsed))
		d.syncMetrics()
		writeJSON(w, map[string]any{"hint": h, "index": d.st.HintsUsed, "total": len(d.Scenario.Hints)})
	})
	mux.HandleFunc("POST /quiz", func(w http.ResponseWriter, r *http.Request) {
		var q results.QuizResult
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		d.st.Quiz = &q
		d.addEvent(fmt.Sprintf("Quiz: %d of %d correct.", q.Correct, q.Total))
		writeJSON(w, q)
	})
	mux.HandleFunc("POST /verify", d.handleVerify)
	mux.HandleFunc("POST /stop", func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		res := d.st.Result(time.Now())
		d.mu.Unlock()
		if err := results.Open(d.Home).Append(res); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, res)
		go func() { time.Sleep(200 * time.Millisecond); d.stop() }()
	})
	return mux
}

// handleVerify streams progress lines, then a final JSON line with the report.
func (d *Daemon) handleVerify(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	switch {
	case d.verifying:
		d.mu.Unlock()
		http.Error(w, "verification is already running", http.StatusConflict)
		return
	case d.st.OverTime(time.Now()):
		d.mu.Unlock()
		http.Error(w, "the time limit has run out; run `opsschool stop` to record your result", http.StatusConflict)
		return
	}
	d.verifying = true
	d.mu.Unlock()
	defer func() { d.mu.Lock(); d.verifying = false; d.since = time.Time{}; d.mu.Unlock() }()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fl, _ := w.(http.Flusher)
	say := func(s string) {
		fmt.Fprintln(w, s)
		if fl != nil {
			fl.Flush()
		}
		d.Log.Print("verify: " + s)
	}
	v := &Verifier{Scenario: d.Scenario, Machine: d.Machine, Engine: d.Engine, Load: d.Load}
	rep, err := v.Run(r.Context(), say)
	if err != nil {
		say("ERROR " + err.Error())
		return
	}
	d.mu.Lock()
	d.st.LastVerify = &rep
	if rep.DataLoss {
		d.st.DataLoss = true
	}
	if rep.Pass {
		now := time.Now()
		// A verified fix leaves the service healthy, so it also mitigates.
		d.passTier(results.TierMitigated, now)
		d.passTier(results.TierFixed, now)
	} else {
		d.addEvent(fmt.Sprintf("Fix verification failed (%d problem(s)).", len(rep.Failures)))
	}
	d.mu.Unlock()
	b, _ := json.Marshal(rep)
	say("RESULT " + string(b))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// notify sends a desktop notification if the platform has a way to.
func notify(title, msg string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("osascript", "-e", fmt.Sprintf("display notification %q with title %q", msg, title))
	case "linux":
		if _, err := exec.LookPath("notify-send"); err != nil {
			return
		}
		cmd = exec.Command("notify-send", title, msg)
	default:
		return
	}
	_ = cmd.Run()
}

// Client talks to the daemon.
type Client struct{ HTTP *http.Client }

func (c Client) base() string { return "http://" + ControlAddr }

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// ErrDaemonDown means the daemon is not answering.
var ErrDaemonDown = errors.New("the session daemon is not running; run `opsschool stop` to clean up")

// Get fetches a JSON endpoint.
func (c Client) Get(path string, v any) error { return c.do(http.MethodGet, path, nil, v) }

// Post posts JSON and decodes the reply.
func (c Client) Post(path string, body, v any) error { return c.do(http.MethodPost, path, body, v) }

func (c Client) do(method, path string, body, v any) error {
	var rd *jsonReader
	if body != nil {
		rd = newJSONReader(body)
	}
	req, err := http.NewRequest(method, c.base()+path, rd.reader())
	if err != nil {
		return err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return ErrDaemonDown
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var b [512]byte
		n, _ := resp.Body.Read(b[:])
		return fmt.Errorf("%s", string(b[:n]))
	}
	if v == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// Stream posts to path and returns the response for line-by-line reading.
func (c Client) Stream(path string) (*http.Response, error) {
	resp, err := (&http.Client{}).Post(c.base()+path, "application/json", nil)
	if err != nil {
		return nil, ErrDaemonDown
	}
	return resp, nil
}
