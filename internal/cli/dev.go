package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/opsschool/simulator/internal/loadgen"
	"github.com/opsschool/simulator/internal/scenario"
	"github.com/opsschool/simulator/internal/telemetry"
)

// Development commands: run pieces of a session on their own.

func init() {
	register("telemetry", "telemetry up|down [--scenario id]", "Start or stop the telemetry stack on its own (development).", runTelemetry)
	register("loadgen", "loadgen [--target url] [--profile p] [--duration d]", "Send shop traffic without a session (development).", runLoadgen)
}

func runTelemetry(e *Env, args []string) error {
	if len(args) == 0 || (args[0] != "up" && args[0] != "down") {
		return errUsage
	}
	fs := newFlags(e, "telemetry")
	id := fs.String("scenario", "", "scenario whose dashboard to load")
	root := fs.String("scenarios", "", "scenarios directory")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	home, err := Home(e)
	if err != nil {
		return err
	}
	stack := telemetry.NewStack(filepath.Join(home, "telemetry"))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if args[0] == "down" {
		return stack.Down(ctx)
	}
	title, sid, extra := "No scenario", "none", []byte(nil)
	if *id != "" {
		dir, err := scenariosRoot(e, *root)
		if err != nil {
			return err
		}
		s, err := scenario.Find(dir, *id)
		if err != nil {
			return err
		}
		title, sid, extra = s.Spec.Title, s.Spec.ID, s.Dashboard
	}
	dash, err := telemetry.Dashboard(sid, title, extra)
	if err != nil {
		return err
	}
	if err := stack.Render(dash); err != nil {
		return err
	}
	if err := stack.Up(ctx); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "Grafana:    %s\nPrometheus: %s\n", telemetry.GrafanaURL(), telemetry.PrometheusURL())
	return nil
}

func runLoadgen(e *Env, args []string) error {
	fs := newFlags(e, "loadgen")
	target := fs.String("target", fmt.Sprintf("http://127.0.0.1:%d", telemetry.ShopPort), "shop base URL")
	profile := fs.String("profile", "steady", "steady or peak")
	rps := fs.Float64("rps", 0, "constant rate instead of a profile")
	dur := fs.Duration("duration", 0, "stop after this long (default: until interrupted)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := loadgen.ProfileByName(*profile, nil)
	if err != nil {
		return err
	}
	if *rps > 0 {
		p = loadgen.Steady(*rps)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if *dur > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *dur)
		defer cancel()
	}
	g := loadgen.New(*target, p)
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				printStats(e, &g.Stats)
			}
		}
	}()
	g.Run(ctx)
	close(done)
	printStats(e, &g.Stats)
	return nil
}

func printStats(e *Env, s *loadgen.Stats) {
	fmt.Fprintf(e.Stdout, "sent %d  ok %d  4xx %d  5xx %d  failed %d\n",
		s.Sent.Load(), s.OK.Load(), s.ClientErr.Load(), s.ServerErr.Load(), s.Failed.Load())
}
