package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/opsschool/emulator/internal/results"
	"github.com/opsschool/emulator/internal/scenario"
	"github.com/opsschool/emulator/internal/session"
	"github.com/opsschool/emulator/internal/telemetry"
	"github.com/opsschool/emulator/internal/vm"
)

func init() {
	register("start", "start <category>/<level>.<n> --user <name>", "Start a scenario: boot the machine, break it, start load and grading.", runStart)
	register("shell", "shell", "Open a root shell in the scenario machine.", runShell)
	register("status", "status [-w]", "Show tier progress, elapsed time and hints used.", runStatus)
	register("hint", "hint", "Point to the curriculum (free), then reveal a hint (costs points).", runHint)
	register("verify", "verify", "Claim a fix: restart, reboot if needed, replay load, then grade the fixed tier.", runVerify)
	register("quiz", "quiz", "Answer the scenario's optional quiz questions.", runQuiz)
	register("stop", "stop", "End the scenario, record your result and tear everything down.", runStop)
	register("_daemon", "_daemon", "(internal) session background process.", runDaemon)
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func driverFlag(fs interface {
	String(string, string, string) *string
}) *string {
	return fs.String("driver", "", "machine driver: lima or container (default: lima if installed)")
}

func runStart(e *Env, args []string) error {
	fs := newFlags(e, "start")
	user := fs.String("user", "", "your username (default: $OPSSCHOOL_USER)")
	seed := fs.Uint64("seed", 0, "session seed (default: random)")
	root := fs.String("scenarios", "", "scenarios directory")
	drv := driverFlag(fs)
	id, rest := splitFirst(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id == "" || fs.NArg() != 0 {
		return errUsage
	}
	if *user == "" {
		*user = e.Getenv("OPSSCHOOL_USER")
	}
	if *user == "" {
		return errors.New("pass --user <name> (or set OPSSCHOOL_USER) so your results are recorded")
	}
	home, err := Home(e)
	if err != nil {
		return err
	}
	if _, err := session.Load(home); err == nil {
		return errors.New("a scenario is already running; run `opsschool stop` first")
	}
	dir, err := scenariosRoot(e, *root)
	if err != nil {
		return err
	}
	s, err := scenario.Find(dir, id)
	if err != nil {
		return err
	}
	if _, ps := scenario.Validate(s.Dir); scenario.HasErrors(ps) {
		return fmt.Errorf("scenario %s is invalid; run `opsschool validate %s`", id, s.Dir)
	}
	m, err := vm.New(firstNonEmpty(*drv, e.Getenv("OPSSCHOOL_DRIVER")))
	if err != nil {
		return err
	}
	if m.Name() != "lima" && s.Spec.NeedsVM != "" {
		// Not the reason: it would give the scenario away.
		return fmt.Errorf("%s needs a virtual machine; start it with --driver lima", s.Spec.ID)
	}
	if *seed == 0 {
		*seed = session.NewSeed()
	}
	absDir, _ := filepath.Abs(s.Dir)
	st := &session.State{
		User: *user, ScenarioID: s.Spec.ID, ScenarioDir: absDir, Level: s.Spec.Level,
		Driver: m.Name(), Image: s.Spec.Image, Seed: *seed,
		Vars:      scenario.ResolveVars(s.Spec.Randomize, *seed),
		TimeLimit: s.Spec.TimeLimit.Duration, TargetTime: s.Spec.TargetTime.Duration,
	}
	ctx, cancel := signalContext()
	defer cancel()
	say := func(msg string) { fmt.Fprintln(e.Stdout, "==> "+msg) }
	env := &session.Env{Home: home, Machine: m, Say: say, Fingerprint: imageFingerprint(filepath.Dir(dir), s.Spec.Image)}

	fail := func(err error) error {
		fmt.Fprintln(e.Stderr, "Setup failed; cleaning up.")
		env.Teardown(context.Background())
		return err
	}
	// Booting is slow and mostly silent, so show a bar while it runs.
	bar := startProgress(e.Stdout, 90*time.Second)
	env.Say = bar.Say
	err = env.Bring(ctx, s)
	bar.Done()
	env.Say = say
	if err != nil {
		return fail(err)
	}
	// The daemon starts load now; it grades and runs the clock only once
	// the scenario begins, after the baseline and the break.
	if err := session.Save(home, st); err != nil {
		return fail(err)
	}
	pid, err := spawnDaemon(home)
	if err != nil {
		session.Clear(home)
		return fail(err)
	}
	st.DaemonPID = pid
	session.Save(home, st)
	abort := func(err error) error {
		syscall.Kill(pid, syscall.SIGTERM)
		session.Clear(home)
		return fail(err)
	}
	if err := waitDaemon(ctx); err != nil {
		return abort(fmt.Errorf("%w (see %s)", err, filepath.Join(session.Dir(home), "daemon.log")))
	}
	if err := (session.Client{}).Post("/baseline", nil, nil); err != nil {
		return abort(err)
	}
	fmt.Fprintf(e.Stdout, "\nYour session page is at\n\n  %s\n\n"+
		"It has a terminal on the server, the dashboards, your progress and hints.\n\n"+
		"The shop is up and serving normal traffic. For the next %s the\n"+
		"dashboards show it healthy, so you have something to compare against once\n"+
		"the scenario begins. The clock hasn't started yet.\n\n", session.PageURL(), shortDuration(session.Baseline))
	if err := countdown(ctx, e.Stdout, "Scenario begins in", session.Baseline); err != nil {
		return abort(err)
	}
	say("Breaking something")
	if err := session.Break(ctx, session.Engine(s, m, st), m, s); err != nil {
		return abort(err)
	}
	if err := (session.Client{}).Post("/begin", nil, nil); err != nil {
		return abort(err)
	}

	printPage(e.Stdout, s)
	fmt.Fprintf(e.Stdout, "Session:    %s\n", session.PageURL())
	fmt.Fprintf(e.Stdout, "Shell:      opsschool shell\n")
	fmt.Fprintf(e.Stdout, "Dashboards: %s\n", telemetry.GrafanaURL())
	fmt.Fprintf(e.Stdout, "Shop:       http://127.0.0.1:%d\n", telemetry.ShopPort)
	fmt.Fprintf(e.Stdout, "Time limit: %s. The clock is running.\n\n", s.Spec.TimeLimit.Duration)
	fmt.Fprintln(e.Stdout, "The mitigated tier is graded continuously. When you think you have fixed")
	fmt.Fprintln(e.Stdout, "the cause, run `opsschool verify`. `opsschool status` shows progress.")
	return nil
}

// printPage shows the scenario the way an on-call engineer would get it: the
// alerts that are firing, then what people have reported.
func printPage(w io.Writer, s *scenario.Scenario) {
	fmt.Fprintf(w, "\nScenario %s (level %d) has begun.\n\n", s.Spec.ID, s.Spec.Level)
	if len(s.Spec.Alerts) == 0 {
		fmt.Fprintln(w, "No alerts are firing. This one was reported by people:")
	} else {
		for _, a := range s.Spec.Alerts {
			fmt.Fprintf(w, "  [FIRING] %s\n", a)
		}
		fmt.Fprintln(w, "\nWhat people are reporting:")
	}
	fmt.Fprintf(w, "\n%s\n\n", indent(wrap(strings.TrimSpace(s.Spec.Summary), 72), "  "))
}

// countdown waits for d, showing the time left. On a terminal the line
// updates in place every second; otherwise it is printed every 30 seconds.
func countdown(ctx context.Context, w io.Writer, label string, d time.Duration) error {
	tty := isTerminal(w)
	end := time.Now().Add(d)
	t := time.NewTicker(time.Second)
	defer t.Stop()
	shown := time.Duration(-1)
	for {
		left := max(0, time.Until(end).Round(time.Second))
		if tty {
			fmt.Fprintf(w, "\r%s %s ", label, clock(left))
		} else if left != shown && (shown < 0 || left%(30*time.Second) == 0) {
			fmt.Fprintf(w, "%s %s\n", label, clock(left))
			shown = left
		}
		if left == 0 {
			break
		}
		select {
		case <-ctx.Done():
			if tty {
				fmt.Fprintln(w)
			}
			return ctx.Err()
		case <-t.C:
		}
	}
	if tty {
		fmt.Fprintln(w)
	}
	return nil
}

// clock formats d as m:ss.
func clock(d time.Duration) string {
	d = d.Round(time.Second)
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

// shortDuration formats d as "2 minutes" or "90 seconds".
func shortDuration(d time.Duration) string {
	if d%time.Minute == 0 {
		if d == time.Minute {
			return "minute"
		}
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	return fmt.Sprintf("%d seconds", int(d.Seconds()))
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func splitFirst(args []string) (string, []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	// Allow flags before the ID.
	for i, a := range args {
		if !strings.HasPrefix(a, "-") && (i == 0 || !flagTakesValue(args[i-1])) {
			return a, append(append([]string{}, args[:i]...), args[i+1:]...)
		}
	}
	return "", args
}

func flagTakesValue(f string) bool {
	if strings.Contains(f, "=") {
		return false
	}
	switch strings.TrimLeft(f, "-") {
	case "user", "seed", "scenarios", "driver":
		return true
	}
	return false
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// spawnDaemon starts `opsschool _daemon` detached from the terminal.
func spawnDaemon(home string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	logf, err := os.OpenFile(filepath.Join(session.Dir(home), "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	defer logf.Close()
	cmd := exec.Command(exe, "_daemon")
	cmd.Env = append(os.Environ(), "OPSSCHOOL_HOME="+home)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	cmd.Process.Release()
	return pid, nil
}

func waitDaemon(ctx context.Context) error {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var st session.Status
		if err := (session.Client{}).Get("/status", &st); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return errors.New("session daemon did not start")
}

func runDaemon(e *Env, args []string) error {
	home, err := Home(e)
	if err != nil {
		return err
	}
	st, err := session.Load(home)
	if err != nil {
		return err
	}
	s, err := scenario.Load(st.ScenarioDir)
	if err != nil {
		return err
	}
	m, err := vm.New(st.Driver)
	if err != nil {
		return err
	}
	gen, err := session.NewGenerator(s)
	if err != nil {
		return err
	}
	d := &session.Daemon{
		Home: home, Log: log.New(e.Stderr, "", log.LstdFlags), Machine: m, Scenario: s,
		Engine: session.Engine(s, m, st), Load: gen,
	}
	ctx, cancel := signalContext()
	defer cancel()
	if m.TelemetryNetwork() != "" {
		host, err := vm.HostOnNetwork(ctx)
		if err != nil {
			return err
		}
		if host != "" {
			d.ExtraListen = fmt.Sprintf("%s:%d", host, telemetry.CLIMetricsPort)
		}
	}
	return d.Run(ctx)
}

func loadRunning(e *Env) (string, *session.State, error) {
	home, err := Home(e)
	if err != nil {
		return "", nil, err
	}
	st, err := session.Load(home)
	return home, st, err
}

func runShell(e *Env, args []string) error {
	if err := noArgs(e, "shell", args); err != nil {
		return err
	}
	_, st, err := loadRunning(e)
	if err != nil {
		return err
	}
	m, err := vm.New(st.Driver)
	if err != nil {
		return err
	}
	return vm.Interactive(m.ShellCommand())
}

func runStatus(e *Env, args []string) error {
	fs := newFlags(e, "status")
	watch := fs.Bool("w", false, "keep watching until interrupted")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, _, err := loadRunning(e); err != nil {
		return err
	}
	for {
		var st session.Status
		if err := (session.Client{}).Get("/status", &st); err != nil {
			return err
		}
		if *watch {
			fmt.Fprint(e.Stdout, "\033[H\033[2J")
		}
		printStatus(e, &st)
		if !*watch {
			return nil
		}
		time.Sleep(5 * time.Second)
	}
}

func printStatus(e *Env, s *session.Status) {
	st := s.State
	if !st.Begun() {
		fmt.Fprintf(e.Stdout, "%s as %s   not begun yet: the shop is running healthy before the scenario starts\n", st.ScenarioID, st.User)
		return
	}
	hint := "not used"
	if st.HintsUsed > 0 {
		hint = "used"
	}
	fmt.Fprintf(e.Stdout, "%s as %s   elapsed %s of %s   hint %s\n\n", st.ScenarioID, st.User, s.Elapsed, st.TimeLimit, hint)
	for _, t := range results.Tiers {
		line := "not yet"
		if at, ok := st.TierPassed[t]; ok {
			line = "PASSED at " + st.Elapsed(at).String()
		} else if t == results.TierMitigated && s.HoldLeft > 0 {
			line = fmt.Sprintf("checks passing; holds in %s", s.HoldLeft)
		} else if t == results.TierFixed && s.Verifying {
			line = "verifying..."
		}
		fmt.Fprintf(e.Stdout, "  %-10s %s\n", t, line)
	}
	if !st.Passed(results.TierMitigated) && len(s.Mitigated) > 0 {
		fmt.Fprintln(e.Stdout, "\nMitigated checks right now:")
		for _, o := range s.Mitigated {
			mark := "fail"
			if o.Pass {
				mark = "ok  "
			}
			fmt.Fprintf(e.Stdout, "  %s %s\n", mark, o.Check.Label())
		}
	}
	if v := st.LastVerify; v != nil && !st.Passed(results.TierFixed) {
		fmt.Fprintf(e.Stdout, "\nLast verification failed:\n")
		for _, f := range v.Failures {
			fmt.Fprintf(e.Stdout, "  - %s\n", f)
		}
	}
	if st.OverTime(time.Now()) {
		fmt.Fprintln(e.Stdout, "\nThe time limit has run out. Run `opsschool stop` to record your result.")
	}
	if n := len(st.Events); n > 0 {
		fmt.Fprintln(e.Stdout, "\nRecent events:")
		for _, ev := range st.Events[max(0, n-5):] {
			fmt.Fprintf(e.Stdout, "  %s  %s\n", ev.At.Local().Format("15:04:05"), ev.Msg)
		}
	}
}

func runHint(e *Env, args []string) error {
	if err := noArgs(e, "hint", args); err != nil {
		return err
	}
	if _, _, err := loadRunning(e); err != nil {
		return err
	}
	var h struct {
		Docs, Hint   string
		Index, Total int
	}
	if err := (session.Client{}).Post("/hint", nil, &h); err != nil {
		return err
	}
	if h.Docs != "" {
		fmt.Fprintf(e.Stdout, "Free hint: the Ops School curriculum covers this. Read\n\n  %s\n", h.Docs)
		if h.Total > 0 {
			fmt.Fprintf(e.Stdout, "\nStill stuck? `opsschool hint` again gives a more direct hint (-%d points).\n", results.HintPenalty)
		}
		return nil
	}
	if h.Hint == "" {
		fmt.Fprintf(e.Stdout, "No more hints (%d used).\n", h.Total)
		return nil
	}
	fmt.Fprintf(e.Stdout, "Hint %d of %d (-%d points):\n\n%s\n", h.Index, h.Total, results.HintPenalty, indent(h.Hint, "  "))
	return nil
}

func runVerify(e *Env, args []string) error {
	if err := noArgs(e, "verify", args); err != nil {
		return err
	}
	if _, _, err := loadRunning(e); err != nil {
		return err
	}
	fmt.Fprintln(e.Stdout, "Verifying your fix. This restarts services and replays load; it takes a few minutes.")
	resp, err := (session.Client{}).Stream("/verify")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b := make([]byte, 512)
		n, _ := resp.Body.Read(b)
		return errors.New(strings.TrimSpace(string(b[:n])))
	}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "RESULT "):
			var rep session.VerifyReport
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "RESULT ")), &rep); err != nil {
				return err
			}
			if rep.Pass {
				fmt.Fprintln(e.Stdout, "\nFixed! The fixed tier passed.")
				return nil
			}
			fmt.Fprintln(e.Stdout, "\nNot fixed yet:")
			for _, f := range rep.Failures {
				fmt.Fprintf(e.Stdout, "  - %s\n", f)
			}
			if rep.DataLoss {
				fmt.Fprintln(e.Stdout, "\nData was lost, so the fixed tier cannot pass in this session.")
			}
			return exitError{1}
		case strings.HasPrefix(line, "ERROR "):
			return errors.New(strings.TrimPrefix(line, "ERROR "))
		default:
			fmt.Fprintln(e.Stdout, "==> "+line)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return errors.New("verification ended without a result; see the session daemon log")
}

func runQuiz(e *Env, args []string) error {
	if err := noArgs(e, "quiz", args); err != nil {
		return err
	}
	_, st, err := loadRunning(e)
	if err != nil {
		return err
	}
	s, err := scenario.Load(st.ScenarioDir)
	if err != nil {
		return err
	}
	if !s.HasQuiz() {
		fmt.Fprintln(e.Stdout, "This scenario has no quiz.")
		return nil
	}
	fmt.Fprintln(e.Stdout, "Optional quiz. It does not change your score.")
	in := bufio.NewReader(e.Stdin)
	correct := 0
	for i, q := range s.Questions {
		fmt.Fprintf(e.Stdout, "\n%d. %s\n", i+1, q.Prompt)
		for j, c := range q.Choices {
			fmt.Fprintf(e.Stdout, "   %d) %s\n", j+1, c)
		}
		for {
			switch q.Type {
			case scenario.QuestionMulti:
				fmt.Fprint(e.Stdout, "Answer (numbers separated by commas): ")
			case scenario.QuestionChoice:
				fmt.Fprint(e.Stdout, "Answer (number): ")
			default:
				fmt.Fprint(e.Stdout, "Answer: ")
			}
			line, err := in.ReadString('\n')
			if err != nil && line == "" {
				return errors.New("quiz cancelled")
			}
			ok, err := q.Check(strings.TrimSpace(line), st.Vars)
			if err != nil {
				fmt.Fprintln(e.Stdout, err)
				continue
			}
			if ok {
				correct++
				fmt.Fprintln(e.Stdout, "Correct.")
			} else {
				fmt.Fprintln(e.Stdout, "Not quite.")
			}
			break
		}
	}
	fmt.Fprintf(e.Stdout, "\n%d of %d correct.\n", correct, len(s.Questions))
	return (session.Client{}).Post("/quiz", results.QuizResult{Correct: correct, Total: len(s.Questions)}, nil)
}

func runStop(e *Env, args []string) error {
	if err := noArgs(e, "stop", args); err != nil {
		return err
	}
	home, st, err := loadRunning(e)
	if err != nil {
		return err
	}
	var res results.Result
	if err := (session.Client{}).Post("/stop", nil, &res); err != nil {
		fmt.Fprintf(e.Stderr, "Could not reach the session daemon (%v); recording the saved state.\n", err)
		res = st.Result(time.Now())
		if err := results.Open(home).Append(res); err != nil {
			return err
		}
		if st.DaemonPID > 0 {
			syscall.Kill(st.DaemonPID, syscall.SIGTERM)
		}
	}
	m, err := vm.New(st.Driver)
	if err != nil {
		return err
	}
	ctx, cancel := signalContext()
	defer cancel()
	env := &session.Env{Home: home, Machine: m, Say: func(s string) { fmt.Fprintln(e.Stdout, "==> "+s) }}
	terr := env.Teardown(ctx)
	if err := session.Clear(home); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "\nResult for %s on %s: %d points\n", res.User, res.Scenario, res.Score)
	for _, t := range results.Tiers {
		if at, ok := res.TierPassed[t]; ok {
			fmt.Fprintf(e.Stdout, "  %-10s passed at %s\n", t, at.Duration())
		} else {
			fmt.Fprintf(e.Stdout, "  %-10s not passed\n", t)
		}
	}
	hint := "not used"
	if res.HintsUsed > 0 {
		hint = "used"
	}
	fmt.Fprintf(e.Stdout, "  hint       %s\n", hint)
	fmt.Fprintf(e.Stdout, "Saved to %s\n", results.Open(home).Path)
	return terr
}
