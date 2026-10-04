// Package telemetry renders and runs the host telemetry stack (Prometheus,
// Loki, Grafana) with Docker Compose.
package telemetry

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	telemetryfs "github.com/opsschool/emulator/telemetry"
)

// Host ports. The VM ports are forwarded by Lima (images/*/lima.yaml).
const (
	PrometheusPort = 19090
	LokiPort       = 13100
	GrafanaPort    = 13000
	CLIMetricsPort = 19999
	ShopPort       = 18080
)

// Versions of the telemetry images.
var Versions = struct{ Prometheus, Loki, Grafana string }{
	Prometheus: "3.15.0",
	Loki:       "3.7.8",
	Grafana:    "13.2.3",
}

// Target is one Prometheus scrape job on the scenario machine. HostPort is
// where Lima forwards VMPort on the host.
type Target struct {
	Job              string
	VMPort, HostPort int
}

// Targets are the scrape jobs for a single-node scenario.
var Targets = []Target{
	{"shop", 9091, 19091},
	{"shop-worker", 9092, 19092},
	{"node", 9100, 19100},
	{"process", 9256, 19256},
	{"mysql", 9104, 19104},
	{"redis", 9121, 19121},
}

type renderedTarget struct{ Job, Instance, Addr, Path string }

// Stack is a rendered telemetry stack in a directory.
type Stack struct {
	Dir string
	// HostNetwork runs the containers on the host network. Needed with
	// Docker Engine on Linux, where containers can't reach ports Lima
	// forwards to 127.0.0.1. Docker Desktop (macOS, Windows, WSL2) reaches
	// them through host.docker.internal.
	HostNetwork bool
	// Network, when set, is an existing Docker network the stack joins to
	// reach the machine directly (the container driver). CLIHost is the
	// host's address on that network, where the CLI serves its metrics.
	Network string
	CLIHost string
}

// UseNetwork makes the stack reach the machine over a Docker network.
func (s *Stack) UseNetwork(network, cliHost string) {
	s.Network, s.CLIHost, s.HostNetwork = network, cliHost, false
}

// NewStack returns a stack for this platform.
func NewStack(dir string) *Stack { return &Stack{Dir: dir, HostNetwork: hostNetworkShared()} }

// hostNetworkShared reports whether containers on Docker's host network
// share this machine's loopback. That holds for Docker Engine on Linux but
// not for Docker Desktop, whose host network is its own VM even when the
// CLI runs on Linux or in WSL2.
func hostNetworkShared() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.OperatingSystem}}").Output()
	// If Docker doesn't answer, starting the stack fails later with a
	// clearer error than this would give.
	return err != nil || !strings.Contains(string(out), "Docker Desktop")
}

// URLs the host uses to reach the stack.
func PrometheusURL() string { return fmt.Sprintf("http://127.0.0.1:%d", PrometheusPort) }
func GrafanaURL() string    { return fmt.Sprintf("http://127.0.0.1:%d", GrafanaPort) }

type netSettings struct {
	HostNetwork             bool
	HostPort, ContainerPort int
	Network, Alias          string
}

type renderData struct {
	*Stack
	Versions      struct{ Prometheus, Loki, Grafana string }
	Ports         struct{ Prometheus, Loki, Grafana int }
	Targets       []renderedTarget
	ListenHost    string
	PrometheusURL string
	LokiURL       string
}

func (d renderData) Net(service string) netSettings {
	port := map[string]int{"prometheus": PrometheusPort, "loki": LokiPort, "grafana": GrafanaPort}[service]
	container := map[string]int{"prometheus": 9090, "loki": LokiPort, "grafana": GrafanaPort}[service]
	n := netSettings{HostNetwork: d.HostNetwork, HostPort: port, ContainerPort: container, Network: d.Network}
	if service == "loki" {
		n.Alias = "host.lima.internal"
	}
	return n
}

func (d renderData) Listen(service string) string {
	if d.HostNetwork {
		return fmt.Sprintf("127.0.0.1:%d", PrometheusPort)
	}
	return "0.0.0.0:9090"
}

func (s *Stack) data() renderData {
	d := renderData{Stack: s, Versions: Versions}
	d.Ports.Prometheus, d.Ports.Loki, d.Ports.Grafana = PrometheusPort, LokiPort, GrafanaPort
	host, cli := "host.docker.internal", "host.docker.internal"
	if s.HostNetwork {
		host, cli = "127.0.0.1", "127.0.0.1"
		d.ListenHost = "127.0.0.1"
		d.PrometheusURL = PrometheusURL()
		d.LokiURL = fmt.Sprintf("http://127.0.0.1:%d", LokiPort)
	} else {
		d.ListenHost = "0.0.0.0"
		d.PrometheusURL = "http://prometheus:9090"
		d.LokiURL = fmt.Sprintf("http://loki:%d", LokiPort)
	}
	if s.Network != "" {
		cli = s.CLIHost
	}
	for _, t := range Targets {
		addr := fmt.Sprintf("%s:%d", host, t.HostPort)
		if s.Network != "" {
			addr = fmt.Sprintf("scenario-vm:%d", t.VMPort)
		}
		d.Targets = append(d.Targets, renderedTarget{t.Job, "scenario-vm", addr, ""})
	}
	daemon := fmt.Sprintf("%s:%d", cli, CLIMetricsPort)
	d.Targets = append(d.Targets,
		renderedTarget{"opsschool", "host", daemon, ""},
		// The session daemon also plays the shop's load balancer.
		renderedTarget{"edge", "edge-1", daemon, "/edge/metrics"})
	return d
}

// Render writes the compose file, configs and the dashboard into s.Dir.
func (s *Stack) Render(dashboard []byte) error {
	d := s.data()
	err := fs.WalkDir(telemetryfs.FS, ".", func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		b, err := fs.ReadFile(telemetryfs.FS, path)
		if err != nil {
			return err
		}
		out := filepath.Join(s.Dir, strings.TrimSuffix(path, ".tmpl"))
		if strings.HasSuffix(path, ".tmpl") {
			t, err := template.New(path).Option("missingkey=error").Parse(string(b))
			if err != nil {
				return err
			}
			var buf bytes.Buffer
			if err := t.Execute(&buf, d); err != nil {
				return fmt.Errorf("render %s: %w", path, err)
			}
			b = buf.Bytes()
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
	if err != nil {
		return err
	}
	dash := filepath.Join(s.Dir, "grafana", "dashboards", "scenario.json")
	if err := os.MkdirAll(filepath.Dir(dash), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dash, dashboard, 0o644)
}

func (s *Stack) compose(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose", "-f", filepath.Join(s.Dir, "docker-compose.yml")}, args...)...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose %s: %w\n%s", strings.Join(args, " "), err, out.String())
	}
	return nil
}

// Up starts the stack and waits until Prometheus, Loki and Grafana answer.
func (s *Stack) Up(ctx context.Context) error {
	if err := s.compose(ctx, "up", "-d", "--remove-orphans"); err != nil {
		return err
	}
	for _, u := range []string{
		PrometheusURL() + "/-/ready",
		fmt.Sprintf("http://127.0.0.1:%d/ready", LokiPort),
		GrafanaURL() + "/api/health",
	} {
		if err := WaitHTTP(ctx, u, 2*time.Minute); err != nil {
			return err
		}
	}
	return nil
}

// Down stops the stack and removes its containers and data.
func (s *Stack) Down(ctx context.Context) error {
	return s.compose(ctx, "down", "-v", "--remove-orphans")
}

// WaitHTTP polls url until it answers 200 or the timeout passes.
func WaitHTTP(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s not ready after %s", url, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
