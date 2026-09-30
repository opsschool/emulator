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

	telemetryfs "github.com/opsschool/simulator/telemetry"
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

// Target is one Prometheus scrape job.
type Target struct {
	Job, Instance string
	Port          int
}

// Targets are the scrape jobs for a single-node scenario.
var Targets = []Target{
	{"shop", "scenario-vm", 19091},
	{"shop-worker", "scenario-vm", 19092},
	{"node", "scenario-vm", 19100},
	{"process", "scenario-vm", 19256},
	{"mysql", "scenario-vm", 19104},
	{"redis", "scenario-vm", 19121},
	{"opsschool", "host", CLIMetricsPort},
}

// Stack is a rendered telemetry stack in a directory.
type Stack struct {
	Dir string
	// HostNetwork runs the containers on the host network. Needed on Linux,
	// where containers can't reach ports Lima forwards to 127.0.0.1. Docker
	// Desktop (macOS, Windows) reaches them through host.docker.internal.
	HostNetwork bool
}

// NewStack returns a stack for this platform.
func NewStack(dir string) *Stack { return &Stack{Dir: dir, HostNetwork: runtime.GOOS == "linux"} }

// URLs the host uses to reach the stack.
func PrometheusURL() string { return fmt.Sprintf("http://127.0.0.1:%d", PrometheusPort) }
func GrafanaURL() string    { return fmt.Sprintf("http://127.0.0.1:%d", GrafanaPort) }

type netSettings struct {
	HostNetwork             bool
	HostPort, ContainerPort int
}

type renderData struct {
	*Stack
	Versions      struct{ Prometheus, Loki, Grafana string }
	Ports         struct{ Prometheus, Loki, Grafana int }
	Targets       []Target
	TargetHost    string
	ListenHost    string
	PrometheusURL string
	LokiURL       string
}

func (d renderData) Net(service string) netSettings {
	port := map[string]int{"prometheus": PrometheusPort, "loki": LokiPort, "grafana": GrafanaPort}[service]
	container := map[string]int{"prometheus": 9090, "loki": LokiPort, "grafana": GrafanaPort}[service]
	return netSettings{HostNetwork: d.HostNetwork, HostPort: port, ContainerPort: container}
}

func (d renderData) Listen(service string) string {
	if d.HostNetwork {
		return fmt.Sprintf("127.0.0.1:%d", PrometheusPort)
	}
	return "0.0.0.0:9090"
}

func (s *Stack) data() renderData {
	d := renderData{Stack: s, Versions: Versions, Targets: Targets}
	d.Ports.Prometheus, d.Ports.Loki, d.Ports.Grafana = PrometheusPort, LokiPort, GrafanaPort
	if s.HostNetwork {
		d.TargetHost, d.ListenHost = "127.0.0.1", "127.0.0.1"
		d.PrometheusURL = PrometheusURL()
		d.LokiURL = fmt.Sprintf("http://127.0.0.1:%d", LokiPort)
	} else {
		d.TargetHost, d.ListenHost = "host.docker.internal", "0.0.0.0"
		d.PrometheusURL = "http://prometheus:9090"
		d.LokiURL = fmt.Sprintf("http://loki:%d", LokiPort)
	}
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
		if err := waitHTTP(ctx, u, 2*time.Minute); err != nil {
			return err
		}
	}
	return nil
}

// Down stops the stack and removes its containers and data.
func (s *Stack) Down(ctx context.Context) error {
	return s.compose(ctx, "down", "-v", "--remove-orphans")
}

func waitHTTP(ctx context.Context, url string, timeout time.Duration) error {
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
