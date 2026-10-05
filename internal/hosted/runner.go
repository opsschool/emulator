// Package hosted runs the emulator as a web service on Kubernetes: a portal
// where learners pick scenarios and see a shared scoreboard, and a session
// runner pod per session that does what `opsschool start` and the session
// daemon do on a laptop. See docs/hosted.md and docs/decisions.md, "Hosted
// mode".
package hosted

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/opsschool/emulator/internal/results"
	"github.com/opsschool/emulator/internal/scenario"
	"github.com/opsschool/emulator/internal/session"
	"github.com/opsschool/emulator/internal/telemetry"
	"github.com/opsschool/emulator/internal/vm"
)

// RunnerPort is where a session runner serves the session page and API.
const RunnerPort = telemetry.CLIMetricsPort

// Runner runs one learner's session inside its session pod.
type Runner struct {
	Scenario *scenario.Scenario
	User     string
	Seed     uint64
	Home     string // session state directory
	Machine  vm.Hosted
	// Token admits the portal's requests; see session.Hosted.
	Token string
	// PortalURL is where results are sent.
	PortalURL string
	// Prefix is the session's path on the portal, such as "/s/abc123/".
	Prefix string
	Log    *log.Logger
}

// Run sets the session up, runs it until it ends, and tears it down.
func (r *Runner) Run(ctx context.Context) error {
	s := r.Scenario
	st := &session.State{
		User: r.User, ScenarioID: s.Spec.ID, ScenarioDir: s.Dir, Level: s.Spec.Level,
		Driver: r.Machine.Name(), Image: s.Spec.Image, Seed: r.Seed,
		Vars:      scenario.ResolveVars(s.Spec.Randomize, r.Seed),
		TimeLimit: s.Spec.TimeLimit.Duration, TargetTime: s.Spec.TargetTime.Duration,
	}
	// Until the daemon takes over the port, the page polls a stand-in
	// that says how setup is going.
	prog := &progress{msg: "Starting your session"}
	starting := &http.Server{Handler: prog, ReadHeaderTimeout: 5 * time.Second}
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", RunnerPort))
	if err != nil {
		return err
	}
	go starting.Serve(ln)
	env := &session.Env{Home: r.Home, Machine: r.Machine, ExternalTelemetry: true, Say: func(m string) {
		r.Log.Print(m)
		prog.set(m)
	}}
	fail := func(err error) error {
		r.Log.Printf("setup failed: %v", err)
		prog.fail(err)
		env.Teardown(context.Background())
		// Leave the error up for the page, then end. The portal removes
		// pods that have ended.
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Minute):
		}
		starting.Close()
		return err
	}

	fwd := &forwarder{log: r.Log}
	if err := fwd.listen(); err != nil {
		return fail(err)
	}
	if err := env.Bring(ctx, s); err != nil {
		return fail(err)
	}
	ip, err := r.Machine.Address(ctx)
	if err != nil {
		return fail(err)
	}
	fwd.setTarget(ip)
	if err := session.WaitForShop(ctx, 2*time.Minute); err != nil {
		return fail(fmt.Errorf("the shop didn't answer through the port forward: %w", err))
	}
	if err := session.Save(r.Home, st); err != nil {
		return fail(err)
	}
	gen, err := session.NewGenerator(s)
	if err != nil {
		return fail(err)
	}
	d := &session.Daemon{
		Home: r.Home, Log: r.Log, Machine: r.Machine, Scenario: s,
		Engine: session.Engine(s, r.Machine, st), Load: gen,
		Hosted: session.Hosted{
			Listen: fmt.Sprintf(":%d", RunnerPort), Token: r.Token, Record: r.record,
			HomeURL: "/", GrafanaURL: r.Prefix + "grafana/d/scenario",
			EndAfter: 10 * time.Minute,
		},
	}
	prog.set("Starting load and grading")
	starting.Close()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	client := session.Client{}
	if err := waitStatus(ctx, client); err != nil {
		return fail(err)
	}
	if err := client.Post("/baseline", nil, nil); err != nil {
		return fail(err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	case <-time.After(session.Baseline):
	}
	r.Log.Print("breaking something")
	if err := session.Break(ctx, session.Engine(s, r.Machine, st), r.Machine, s); err != nil {
		r.Log.Printf("break failed: %v", err)
		env.Teardown(context.Background())
		return err
	}
	if err := client.Post("/begin", nil, nil); err != nil {
		return err
	}
	err = <-done
	if ctx.Err() != nil {
		// The pod is being deleted; the machine goes with it.
		env.Teardown(context.Background())
	}
	return err
}

func waitStatus(ctx context.Context, c session.Client) error {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var st session.Status
		if err := c.Get("/status", &st); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return errors.New("the session daemon did not start")
}

// record sends a finished session's result to the portal.
func (r *Runner) record(res results.Result) error {
	b, err := json.Marshal(res)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimSuffix(r.PortalURL, "/")+"/internal/results", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set(session.TokenHeader, r.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("saving your result: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("saving your result: %s", strings.TrimSpace(string(b)))
	}
	return nil
}

// Starting is the /status reply while a session is being set up. The page
// shows Message, or Error when setup failed.
type Starting struct {
	Starting bool   `json:"starting"`
	Message  string `json:"message"`
	Error    string `json:"error,omitempty"`
}

// progress serves Starting until the daemon takes over.
type progress struct {
	mu       sync.Mutex
	msg, err string
}

func (p *progress) set(m string) { p.mu.Lock(); p.msg = m; p.mu.Unlock() }

func (p *progress) fail(err error) { p.mu.Lock(); p.err = err.Error(); p.mu.Unlock() }

func (p *progress) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	s := Starting{Starting: true, Message: p.msg, Error: p.err}
	p.mu.Unlock()
	writeStarting(w, s)
}

func writeStarting(w http.ResponseWriter, s Starting) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(s)
}

// forwarder listens on the loopback ports that Lima would forward from a
// VM, and relays them to the machine pod, so the load generator, checks and
// telemetry reach the machine at the addresses they use on a laptop.
type forwarder struct {
	log    *log.Logger
	mu     sync.Mutex
	target string // the machine pod's IP
}

// forwards maps a loopback port to the machine port behind it.
func forwards() map[int]int {
	m := map[int]int{telemetry.ShopPort: 80}
	for _, t := range telemetry.Targets {
		m[t.HostPort] = t.VMPort
	}
	return m
}

func (f *forwarder) setTarget(ip string) { f.mu.Lock(); f.target = ip; f.mu.Unlock() }

func (f *forwarder) listen() error {
	for local, remote := range forwards() {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", local))
		if err != nil {
			return err
		}
		go f.serve(ln, remote)
	}
	return nil
}

func (f *forwarder) serve(ln net.Listener, port int) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go f.relay(c, port)
	}
}

func (f *forwarder) relay(c net.Conn, port int) {
	defer c.Close()
	f.mu.Lock()
	ip := f.target
	f.mu.Unlock()
	if ip == "" {
		return
	}
	m, err := net.DialTimeout("tcp", net.JoinHostPort(ip, fmt.Sprint(port)), 5*time.Second)
	if err != nil {
		return
	}
	defer m.Close()
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		io.Copy(dst, src)
		if tc, ok := dst.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
		done <- struct{}{}
	}
	go pipe(m, c)
	go pipe(c, m)
	<-done
	<-done
}
