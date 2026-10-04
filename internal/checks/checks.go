// Package checks evaluates scenario checks: HTTP probes and PromQL from the
// host, and scripts inside the scenario machine.
package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/opsschool/emulator/internal/scenario"
	"github.com/opsschool/emulator/internal/vm"
)

// StateDir is where script checks keep state inside the machine.
const StateDir = "/var/lib/opsschool/state"

// ScriptDir is where scenario scripts are copied to run. It is in memory
// (/run is a tmpfs), so the harness keeps working on a full disk.
const ScriptDir = "/run/opsschool"

// Phases passed to script checks as OPSSCHOOL_PHASE.
const (
	PhaseBaseline = "baseline"
	PhaseCheck    = "check"
)

// Engine evaluates checks for one session.
type Engine struct {
	Scenario   *scenario.Scenario
	Machine    vm.Driver
	Prometheus string            // base URL
	Template   map[string]string // data for {{key}} references
	Env        []string          // OPSSCHOOL_* variables for scripts
	HTTP       *http.Client
}

// Outcome is the result of one check.
type Outcome struct {
	Check  scenario.Check
	Pass   bool
	Detail string // why it failed, or what it saw
}

// Evaluate runs one check. An error means the check could not be run at
// all; it counts as a failure.
func (e *Engine) Evaluate(ctx context.Context, c scenario.Check, phase string) Outcome {
	o := Outcome{Check: c}
	var err error
	switch c.Type {
	case scenario.CheckHTTP:
		o.Pass, o.Detail, err = e.http(ctx, c)
	case scenario.CheckPromQL:
		o.Pass, o.Detail, err = e.promql(ctx, c)
	case scenario.CheckScript:
		o.Pass, o.Detail, err = e.script(ctx, c, phase)
	default:
		err = fmt.Errorf("unknown check type %q", c.Type)
	}
	if err != nil {
		o.Pass, o.Detail = false, err.Error()
	}
	return o
}

// EvaluateAll runs checks in order and reports whether all passed.
func (e *Engine) EvaluateAll(ctx context.Context, cs []scenario.Check, phase string) (bool, []Outcome) {
	all := true
	out := make([]Outcome, 0, len(cs))
	for _, c := range cs {
		o := e.Evaluate(ctx, c, phase)
		all = all && o.Pass
		out = append(out, o)
	}
	return all, out
}

func (e *Engine) client() *http.Client {
	if e.HTTP != nil {
		return e.HTTP
	}
	return &http.Client{Timeout: 5 * time.Second}
}

func (e *Engine) http(ctx context.Context, c scenario.Check) (bool, string, error) {
	u, err := scenario.Render(c.URL, e.Template)
	if err != nil {
		return false, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, "", err
	}
	resp, err := e.client().Do(req)
	if err != nil {
		return false, "request failed: " + err.Error(), nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if c.ExpectStatus != 0 && resp.StatusCode != c.ExpectStatus {
		return false, fmt.Sprintf("status %d, want %d", resp.StatusCode, c.ExpectStatus), nil
	}
	if c.ExpectBody != "" {
		want, err := scenario.Render(c.ExpectBody, e.Template)
		if err != nil {
			return false, "", err
		}
		if !strings.Contains(string(body), want) {
			return false, fmt.Sprintf("body does not contain %q", want), nil
		}
	}
	return true, fmt.Sprintf("status %d", resp.StatusCode), nil
}

// promql passes when the query returns at least one sample. Write checks
// as filters, such as `ratio < 0.01`, which return nothing when false. (A
// filter that holds can return 0, so sample values are not inspected.)
func (e *Engine) promql(ctx context.Context, c scenario.Check) (bool, string, error) {
	expr, err := scenario.Render(c.Expr, e.Template)
	if err != nil {
		return false, "", err
	}
	vals, err := Query(ctx, e.client(), e.Prometheus, expr)
	if err != nil {
		return false, "", err
	}
	if len(vals) == 0 {
		return false, "no result", nil
	}
	return true, fmt.Sprintf("value %g", vals[0]), nil
}

// Query runs an instant PromQL query and returns the sample values.
func Query(ctx context.Context, client *http.Client, base, expr string) ([]float64, error) {
	u := base + "/api/v1/query?" + url.Values{"query": {expr}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prometheus: %w", err)
	}
	defer resp.Body.Close()
	var r struct {
		Status string
		Error  string
		Data   struct {
			ResultType string          `json:"resultType"`
			Result     json.RawMessage `json:"result"`
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("prometheus: %w", err)
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("prometheus: %s", r.Error)
	}
	var raw [][2]any
	switch r.Data.ResultType {
	case "vector":
		var vs []struct{ Value [2]any }
		if err := json.Unmarshal(r.Data.Result, &vs); err != nil {
			return nil, err
		}
		for _, v := range vs {
			raw = append(raw, v.Value)
		}
	case "scalar":
		var v [2]any
		if err := json.Unmarshal(r.Data.Result, &v); err != nil {
			return nil, err
		}
		raw = append(raw, v)
	default:
		return nil, fmt.Errorf("prometheus: unsupported result type %q", r.Data.ResultType)
	}
	out := make([]float64, 0, len(raw))
	for _, v := range raw {
		s, _ := v[1].(string)
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("prometheus: bad value %q", s)
		}
		out = append(out, f)
	}
	return out, nil
}

// script copies the check script into the machine and runs it there.
func (e *Engine) script(ctx context.Context, c scenario.Check, phase string) (bool, string, error) {
	remote := ScriptDir + "/checks/" + path.Base(c.Run)
	if err := e.Machine.CopyIn(ctx, e.Scenario.Path(c.Run), remote); err != nil {
		return false, "", err
	}
	env := append(append([]string{}, e.Env...), "OPSSCHOOL_PHASE="+phase, "OPSSCHOOL_STATE_DIR="+StateDir)
	res, err := e.Machine.Run(ctx, "mkdir -p "+StateDir+" && bash "+remote, env)
	if err != nil {
		return false, "", err
	}
	if res.ExitCode != 0 {
		detail := fmt.Sprintf("exit %d", res.ExitCode)
		if s := strings.TrimSpace(res.Stderr + res.Stdout); s != "" {
			detail += ": " + lastLine(s)
		}
		return false, detail, nil
	}
	return true, "exit 0", nil
}

func lastLine(s string) string {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}
