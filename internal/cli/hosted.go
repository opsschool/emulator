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
	machines := fs.String("machines", "kubevirt", "how to run scenario machines: kubevirt (a VM each; needs KubeVirt and KVM nodes), ec2 (an EC2 instance each) or pods (privileged pods; some scenarios can't run)")
	machineImage := fs.String("machine-image", "", `scenario machine image; "{image}" becomes the scenario's image name (default: opsschool/{image}-vm:base for kubevirt, opsschool/{image}:base for pods, the newest AMI built for the image for ec2)`)
	ec2Subnet := fs.String("ec2-subnet", "", "subnet for machine instances (ec2 only)")
	ec2Groups := fs.String("ec2-security-groups", "", "comma-separated security groups for machine instances (ec2 only)")
	ec2Type := fs.String("ec2-instance-type", vm.EC2InstanceType, "instance type for machines (ec2 only)")
	portalURL := fs.String("portal-url", "", "the portal's address inside the cluster, for results (default: http://opsschool.<namespace>.svc:8080)")
	sa := fs.String("session-service-account", "opsschool-session", "service account for session pods")
	runtimeClass := fs.String("runtime-class", "", "runtime class for machine pods, such as kata (pods only)")
	cpu := fs.String("machine-cpu", "1", "CPU request for each machine")
	mem := fs.String("machine-memory", "2Gi", "memory request for each machine pod (pods only; VMs get "+vm.VMMemory+")")
	memLimit := fs.String("machine-memory-limit", "", "memory limit for each machine pod (pods only; default: none)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errUsage
	}
	var driver string
	machineEnv := map[string]string{}
	switch *machines {
	case "kubevirt":
		driver = "kubevirt"
		if *machineImage == "" {
			*machineImage = vm.VMDiskImage("{image}")
		}
	case "pods":
		driver = "kubernetes"
		if *machineImage == "" {
			*machineImage = vm.BaseImage("{image}")
		}
	case "ec2":
		driver = "ec2"
		if *ec2Subnet == "" || *ec2Groups == "" {
			return errors.New("--machines=ec2 needs --ec2-subnet and --ec2-security-groups")
		}
		machineEnv["OPSSCHOOL_EC2_SUBNET"] = *ec2Subnet
		machineEnv["OPSSCHOOL_EC2_SECURITY_GROUPS"] = *ec2Groups
		machineEnv["OPSSCHOOL_EC2_INSTANCE_TYPE"] = *ec2Type
		machineEnv["OPSSCHOOL_MAX_AGE"] = maxAge.String()
	default:
		return fmt.Errorf("--machines is kubevirt, ec2 or pods, not %q", *machines)
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
		if s.Spec.NeedsVM != "" && driver == "kubernetes" {
			fmt.Fprintf(e.Stderr, "skipping %s: it only runs on a VM: %s\n", s.Spec.ID, s.Spec.NeedsVM)
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
			ServiceAccount: *sa, MachineDriver: driver, RuntimeClass: *runtimeClass,
			MachineCPU: *cpu, MachineMemory: *mem, MachineMemoryLimit: *memLimit, MachineEnv: machineEnv,
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
	lg.Printf("serving %d scenarios on %s (sessions in namespace %s, machines: %s)", len(valid), *listen, *ns, *machines)
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
	d, err := vm.New(firstNonEmpty(e.Getenv("OPSSCHOOL_MACHINE_DRIVER"), "kubernetes"))
	if err != nil {
		return err
	}
	m, ok := d.(vm.Hosted)
	if !ok {
		return fmt.Errorf("the %s driver can't run hosted sessions", d.Name())
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
