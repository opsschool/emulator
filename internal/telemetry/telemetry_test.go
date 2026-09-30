package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashboard(t *testing.T) {
	extra := []byte(`{"panels": [{"type": "timeseries", "title": "Scenario panel", "gridPos": {"w": 24, "h": 6}}]}`)
	b, err := Dashboard("linux-disk-full", "Disk full", extra)
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Refresh string
		Panels  []struct {
			ID      int
			Type    string
			Title   string
			GridPos struct{ X, Y, W, H int }
		}
	}
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if d.Refresh != "5s" {
		t.Errorf("refresh %q", d.Refresh)
	}
	ids := map[int]bool{}
	var rows []string
	for _, p := range d.Panels {
		if ids[p.ID] {
			t.Errorf("duplicate panel id %d", p.ID)
		}
		ids[p.ID] = true
		if p.GridPos.X+p.GridPos.W > 24 {
			t.Errorf("panel %q overflows the grid: %+v", p.Title, p.GridPos)
		}
		if p.Type == "row" {
			rows = append(rows, p.Title)
		}
	}
	want := "Tier status,Service (RED),Host (USE),MySQL,Redis,Logs,Scenario"
	if got := strings.Join(rows, ","); got != want {
		t.Errorf("rows %s, want %s", got, want)
	}
	if !strings.Contains(string(b), `opsschool_check_passed{scenario=\"linux-disk-full\",tier=\"mitigated\"}`) {
		t.Error("tier status query missing")
	}
	if _, err := Dashboard("x", "x", []byte("{")); err == nil {
		t.Error("expected error for bad scenario dashboard")
	}
}

func TestRender(t *testing.T) {
	nd := t.TempDir()
	ns := &Stack{Dir: nd}
	ns.UseNetwork("opsschool", "172.30.0.1")
	if err := ns.Render([]byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	c, _ := os.ReadFile(filepath.Join(nd, "docker-compose.yml"))
	p, _ := os.ReadFile(filepath.Join(nd, "prometheus.yml"))
	for _, want := range []string{"external: true", "aliases: [telemetry.opsschool.internal]"} {
		if !strings.Contains(string(c), want) {
			t.Errorf("network compose missing %q:\n%s", want, c)
		}
	}
	for _, want := range []string{"scenario-vm:9100", "172.30.0.1:19999"} {
		if !strings.Contains(string(p), want) {
			t.Errorf("network prometheus.yml missing %q:\n%s", want, p)
		}
	}
	for _, host := range []bool{true, false} {
		dir := t.TempDir()
		s := &Stack{Dir: dir, HostNetwork: host}
		if err := s.Render([]byte(`{}`)); err != nil {
			t.Fatal(err)
		}
		compose, _ := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
		prom, _ := os.ReadFile(filepath.Join(dir, "prometheus.yml"))
		ds, _ := os.ReadFile(filepath.Join(dir, "grafana/provisioning/datasources/datasources.yml"))
		if host != strings.Contains(string(compose), "network_mode: host") {
			t.Errorf("host=%v compose:\n%s", host, compose)
		}
		wantTarget := map[bool]string{true: "127.0.0.1:19100", false: "host.docker.internal:19100"}[host]
		if !strings.Contains(string(prom), wantTarget) {
			t.Errorf("prometheus.yml missing %s:\n%s", wantTarget, prom)
		}
		if !strings.Contains(string(ds), "uid: prometheus") {
			t.Errorf("datasources:\n%s", ds)
		}
		if _, err := os.Stat(filepath.Join(dir, "grafana/dashboards/scenario.json")); err != nil {
			t.Error(err)
		}
	}
}
