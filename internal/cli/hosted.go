package cli

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/opsschool/emulator/internal/hosted"
	"github.com/opsschool/emulator/internal/results"
	"github.com/opsschool/emulator/internal/scenario"
	"github.com/opsschool/emulator/internal/session"
	"github.com/opsschool/emulator/internal/vm"
)

func init() {
	register("serve", "serve [flags]", "Run the hosted portal on Kubernetes (see docs/hosted.md).", runServe)
	register("_runner", "_runner", "(internal) run a hosted session inside its pod.", runRunner)
	register("_render-telemetry", "_render-telemetry <id> <dir>", "(internal) write a hosted session's telemetry config.", runRenderTelemetry)
}

func runServe(e *Env, args []string) error {
	fs := newFlags(e, "serve")
	env := func(name, def string) string { return firstNonEmpty(e.Getenv(name), def) }
	listen := fs.String("listen", ":8080", "address to serve on")
	data := fs.String("data", env("OPSSCHOOL_HOME", "/var/lib/opsschool"), "directory for the shared results file")
	root := fs.String("scenarios", "", "scenarios directory")
	userHeader := fs.String("user-header", "", "header an authenticating proxy sets to the learner's name, such as X-Forwarded-Email (default: learners type a name)")
	publicURL := fs.String("public-url", "", "the portal's address as browsers see it (default: from each request)")
	maxSessions := fs.Int("max-sessions", 20, "most sessions running at once; 0 for no cap")
	maxAge := fs.Duration("max-age", 3*time.Hour, "delete sessions older than this")
	ns := fs.String("namespace", env("OPSSCHOOL_NAMESPACE", ""), "namespace for session pods (default: the portal's)")
	image := fs.String("image", env("OPSSCHOOL_IMAGE", ""), "the opsschool image, for session runners")
	machineImage := fs.String("machine-image", "opsschool/{image}:base", `scenario machine image; "{image}" becomes the scenario's image name`)
	portalURL := fs.String("portal-url", "", "the portal's address inside the cluster, for results (default: http://opsschool.<namespace>.svc:8080)")
	sa := fs.String("session-service-account", "opsschool-session", "service account for session pods")
	runtimeClass := fs.String("runtime-class", "", "runtime class for machine pods, such as kata")
	cpu := fs.String("machine-cpu", "1", "CPU request for each machine pod")
	mem := fs.String("machine-memory", "2Gi", "memory request for each machine pod")
	memLimit := fs.String("machine-memory-limit", "", "memory limit for each machine pod (default: none)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errUsage
	}
	if *image == "" {
		return errors.New("pass --image (or set OPSSCHOOL_IMAGE) to the opsschool image the portal runs")
	}
	if *ns == "" {
		b, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err != nil {
			return errors.New("pass --namespace; the portal isn't running in a cluster")
		}
		*ns = strings.TrimSpace(string(b))
	}
	if *portalURL == "" {
		*portalURL = fmt.Sprintf("http://opsschool.%s.svc:8080", *ns)
	}
	dir, err := scenariosRoot(e, *root)
	if err != nil {
		return err
	}
	scs, errs := scenario.LoadAll(dir)
	for _, err := range errs {
		fmt.Fprintf(e.Stderr, "skipping: %v\n", err)
	}
	var valid []*scenario.Scenario
	for _, s := range scs {
		if _, ps := scenario.Validate(s.Dir); scenario.HasErrors(ps) {
			fmt.Fprintf(e.Stderr, "skipping %s: it doesn't validate\n", s.Spec.ID)
			continue
		}
		valid = append(valid, s)
	}
	if len(valid) == 0 {
		return hosted.ErrNoScenarios
	}
	lg := log.New(e.Stderr, "", log.LstdFlags)
	p := &hosted.Portal{
		Scenarios: valid, Store: results.Open(*data), UserHeader: *userHeader, PublicURL: *publicURL,
		MaxSessions: *maxSessions, MaxAge: *maxAge, Log: lg,
		Backend: &hosted.Kube{
			Namespace: *ns, Image: *image, MachineImage: *machineImage, PortalURL: *portalURL,
			ServiceAccount: *sa, RuntimeClass: *runtimeClass,
			MachineCPU: *cpu, MachineMemory: *mem, MachineMemoryLimit: *memLimit,
		},
	}
	ctx, cancel := signalContext()
	defer cancel()
	go p.Run(ctx)
	srv := &http.Server{Addr: *listen, Handler: p.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		srv.Shutdown(sctx)
	}()
	lg.Printf("serving %d scenarios on %s (sessions in namespace %s)", len(valid), *listen, *ns)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func runRunner(e *Env, args []string) error {
	if err := noArgs(e, "_runner", args); err != nil {
		return err
	}
	home, err := Home(e)
	if err != nil {
		return err
	}
	dir, err := scenariosRoot(e, "")
	if err != nil {
		return err
	}
	s, err := scenario.Find(dir, e.Getenv("OPSSCHOOL_SCENARIO"))
	if err != nil {
		return err
	}
	m, err := vm.KubeFromEnv()
	if err != nil {
		return err
	}
	r := &hosted.Runner{
		Scenario: s, User: e.Getenv("OPSSCHOOL_USER"), Seed: session.NewSeed(), Home: home, Machine: m,
		Token: e.Getenv("OPSSCHOOL_TOKEN"), PortalURL: e.Getenv("OPSSCHOOL_PORTAL_URL"),
		Prefix: e.Getenv("OPSSCHOOL_PREFIX"), Log: log.New(e.Stderr, "", log.LstdFlags),
	}
	ctx, cancel := signalContext()
	defer cancel()
	err = r.Run(ctx)
	if err != nil {
		// Kubernetes keeps this in the pod's status, where the portal
		// reads it for its log. Best effort: there's nothing to do if it
		// fails.
		os.WriteFile("/dev/termination-log", []byte(err.Error()), 0o644)
	}
	return err
}

func runRenderTelemetry(e *Env, args []string) error {
	if len(args) != 2 {
		return errUsage
	}
	dir, err := scenariosRoot(e, "")
	if err != nil {
		return err
	}
	s, err := scenario.Find(dir, args[0])
	if err != nil {
		return err
	}
	return session.RenderTelemetry(args[1], s)
}
